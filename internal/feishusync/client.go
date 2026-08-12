package feishusync

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type CommandRunner interface {
	Run(ctx context.Context, stdin string, args ...string) ([]byte, error)
}

type ExecRunner struct {
	Path    string
	Timeout time.Duration
}

func (r ExecRunner) Run(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	path := r.Path
	if path == "" {
		var err error
		path, err = exec.LookPath("lark-cli")
		if err != nil {
			return nil, fmt.Errorf("find lark-cli: %w", err)
		}
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.Command(path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start lark-cli: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			return stdout.Bytes(), nil
		}
		message := strings.TrimSpace(stderr.String())
		if len(message) > 4096 {
			message = message[:4096]
		}
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("lark-cli %s failed: %s", strings.Join(args[:min(2, len(args))], " "), message)
	case <-runCtx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("lark-cli timed out after %s", timeout)
		}
		return nil, runCtx.Err()
	}
}

type Client struct {
	Runner CommandRunner
}

type Document struct {
	ID       string
	URL      string
	Revision int64
	Content  string
}

type SyncResult struct {
	Revision int64
	Hash     string
}

func (c Client) Bind(ctx context.Context, docRef string, rendered Rendered) (SyncResult, error) {
	outline, err := c.fetch(ctx, docRef, "outline", "", "with-ids")
	if err != nil {
		return SyncResult{}, err
	}
	headingIDs, err := findHeadingIDs(outline.Content, ManagedHeading)
	if err != nil {
		return SyncResult{}, fmt.Errorf("parse document outline: %w", err)
	}
	switch len(headingIDs) {
	case 0:
		content := rendered.ManagedXML + `<h1>同步说明</h1><p>此区域由 InfoWall 单向同步；飞书同步失败不会回滚本地数据。</p>`
		raw, updateErr := c.run(ctx, content, "docs", "+update", "--as", "user", "--doc", docRef, "--command", "block_insert_after", "--block-id", "-1", "--revision-id", strconv.FormatInt(outline.Revision, 10), "--content", "-", "--format", "json")
		if updateErr != nil {
			return SyncResult{}, updateErr
		}
		revision, decodeErr := decodeRevision(raw)
		if decodeErr != nil {
			return SyncResult{}, fmt.Errorf("decode block_insert_after response: %w", decodeErr)
		}
		verify, fetchErr := c.fetch(ctx, docRef, "outline", "", "with-ids")
		if fetchErr != nil {
			return SyncResult{}, fetchErr
		}
		verifyIDs, findErr := findHeadingIDs(verify.Content, ManagedHeading)
		if findErr != nil || len(verifyIDs) != 1 {
			return SyncResult{}, fmt.Errorf("verify bound managed heading: ids=%d err=%v", len(verifyIDs), findErr)
		}
		verifiedRevision, verifyErr := c.verifyManagedSection(ctx, docRef, rendered.Hash)
		if verifyErr != nil {
			return SyncResult{}, fmt.Errorf("verify bound document: %w", verifyErr)
		}
		return SyncResult{Revision: max64(max64(revision, verify.Revision), verifiedRevision), Hash: rendered.Hash}, nil
	case 1:
		return c.SyncManagedSection(ctx, docRef, rendered)
	default:
		return SyncResult{}, fmt.Errorf("managed heading %q count is %d, want at most 1", ManagedHeading, len(headingIDs))
	}
}

func (c Client) Create(ctx context.Context, rendered Rendered) (Document, error) {
	raw, err := c.run(ctx, InitialDocument(rendered), "docs", "+create", "--as", "user", "--content", "-", "--format", "json")
	if err != nil {
		return Document{}, err
	}
	doc, err := decodeDocument(raw)
	if err != nil {
		return Document{}, fmt.Errorf("decode create response: %w", err)
	}
	if doc.ID == "" || doc.URL == "" {
		return Document{}, fmt.Errorf("create response missing document id or url")
	}
	verifiedRevision, err := c.verifyManagedSection(ctx, doc.ID, rendered.Hash)
	if err != nil {
		return Document{}, fmt.Errorf("verify created document: %w", err)
	}
	doc.Revision = max64(doc.Revision, verifiedRevision)
	return doc, nil
}

func (c Client) verifyManagedSection(ctx context.Context, docRef, hash string) (int64, error) {
	outline, err := c.fetch(ctx, docRef, "outline", "", "with-ids")
	if err != nil {
		return 0, err
	}
	ids, err := findHeadingIDs(outline.Content, ManagedHeading)
	if err != nil || len(ids) != 1 {
		return 0, fmt.Errorf("managed heading ids=%d err=%v", len(ids), err)
	}
	section, err := c.fetch(ctx, docRef, "section", ids[0], "full")
	if err != nil {
		return 0, err
	}
	if strings.Count(section.Content, EndMarkerPrefix+hash) != 1 {
		return 0, fmt.Errorf("managed section hash %s was not read back exactly once", hash)
	}
	return max64(outline.Revision, section.Revision), nil
}

func (c Client) SyncManagedSection(ctx context.Context, docRef string, rendered Rendered) (SyncResult, error) {
	outline, err := c.fetch(ctx, docRef, "outline", "", "with-ids")
	if err != nil {
		return SyncResult{}, err
	}
	headingIDs, err := findHeadingIDs(outline.Content, ManagedHeading)
	if err != nil {
		return SyncResult{}, fmt.Errorf("parse document outline: %w", err)
	}
	if len(headingIDs) != 1 {
		return SyncResult{}, fmt.Errorf("managed heading %q count is %d, want exactly 1", ManagedHeading, len(headingIDs))
	}

	replaceRaw, err := c.run(ctx, rendered.ManagedXML, "docs", "+update", "--as", "user", "--doc", docRef, "--command", "block_replace", "--block-id", headingIDs[0], "--revision-id", strconv.FormatInt(outline.Revision, 10), "--content", "-", "--format", "json")
	if err != nil {
		return SyncResult{}, err
	}
	replaceRevision, err := decodeRevision(replaceRaw)
	if err != nil {
		return SyncResult{}, fmt.Errorf("decode block_replace response: %w", err)
	}

	// block_replace invalidates the heading ID. Fetch the outline and section
	// again before the cleanup operation so no stale ID is reused.
	newOutline, err := c.fetch(ctx, docRef, "outline", "", "with-ids")
	if err != nil {
		return SyncResult{}, err
	}
	newHeadingIDs, err := findHeadingIDs(newOutline.Content, ManagedHeading)
	if err != nil || len(newHeadingIDs) != 1 {
		return SyncResult{}, fmt.Errorf("locate replaced managed heading: ids=%d err=%v", len(newHeadingIDs), err)
	}
	section, err := c.fetch(ctx, docRef, "section", newHeadingIDs[0], "full")
	if err != nil {
		return SyncResult{}, err
	}
	blocks, err := parseTopLevelBlocks(section.Content)
	if err != nil {
		return SyncResult{}, fmt.Errorf("parse replaced managed section: %w", err)
	}
	// block_replace inserts the new managed section before the old sibling
	// blocks. When the rendered content is unchanged, both sections temporarily
	// contain the same marker. The first marker is the new boundary; every block
	// after it is stale and must be deleted, including duplicate markers.
	markerIndex := -1
	for i, block := range blocks {
		if strings.Contains(block.Text, EndMarkerPrefix+rendered.Hash) {
			if markerIndex == -1 {
				markerIndex = i
			}
		}
	}
	if markerIndex == -1 {
		return SyncResult{}, fmt.Errorf("managed section missing sync marker %s", rendered.Hash)
	}
	stale := make([]string, 0, len(blocks)-markerIndex-1)
	for _, block := range blocks[markerIndex+1:] {
		if block.ID != "" {
			stale = append(stale, block.ID)
		}
	}
	finalRevision := max64(replaceRevision, section.Revision)
	if len(stale) > 0 {
		deleteRaw, deleteErr := c.run(ctx, "", "docs", "+update", "--as", "user", "--doc", docRef, "--command", "block_delete", "--block-id", strings.Join(stale, ","), "--revision-id", strconv.FormatInt(section.Revision, 10), "--format", "json")
		if deleteErr != nil {
			return SyncResult{}, deleteErr
		}
		finalRevision, err = decodeRevision(deleteRaw)
		if err != nil {
			return SyncResult{}, fmt.Errorf("decode block_delete response: %w", err)
		}
	}

	verifyOutline, err := c.fetch(ctx, docRef, "outline", "", "with-ids")
	if err != nil {
		return SyncResult{}, err
	}
	verifyIDs, err := findHeadingIDs(verifyOutline.Content, ManagedHeading)
	if err != nil || len(verifyIDs) != 1 {
		return SyncResult{}, fmt.Errorf("verify managed heading: ids=%d err=%v", len(verifyIDs), err)
	}
	verified, err := c.fetch(ctx, docRef, "section", verifyIDs[0], "full")
	if err != nil {
		return SyncResult{}, err
	}
	if strings.Count(verified.Content, EndMarkerPrefix+rendered.Hash) != 1 {
		return SyncResult{}, fmt.Errorf("readback verification failed for sync hash %s", rendered.Hash)
	}
	return SyncResult{Revision: max64(finalRevision, verified.Revision), Hash: rendered.Hash}, nil
}

func (c Client) fetch(ctx context.Context, docRef, scope, startID, detail string) (Document, error) {
	args := []string{"docs", "+fetch", "--as", "user", "--doc", docRef, "--scope", scope, "--detail", detail, "--format", "json"}
	if startID != "" {
		args = append(args, "--start-block-id", startID)
	}
	raw, err := c.run(ctx, "", args...)
	if err != nil {
		return Document{}, err
	}
	doc, err := decodeDocument(raw)
	if err != nil {
		return Document{}, fmt.Errorf("decode fetch response: %w", err)
	}
	return doc, nil
}

func (c Client) run(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	if c.Runner == nil {
		return nil, fmt.Errorf("lark command runner is not configured")
	}
	return c.Runner.Run(ctx, stdin, args...)
}

func decodeDocument(raw []byte) (Document, error) {
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Document struct {
				ID       string `json:"document_id"`
				URL      string `json:"url"`
				Revision int64  `json:"revision_id"`
				Content  string `json:"content"`
			} `json:"document"`
		} `json:"data"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return Document{}, err
	}
	if !envelope.OK {
		return Document{}, fmt.Errorf("lark response not ok: %v", envelope.Error)
	}
	d := envelope.Data.Document
	return Document{ID: d.ID, URL: d.URL, Revision: d.Revision, Content: d.Content}, nil
}

func decodeRevision(raw []byte) (int64, error) {
	doc, err := decodeDocument(raw)
	if err != nil {
		return 0, err
	}
	return doc.Revision, nil
}

type parsedBlock struct {
	ID   string
	Name string
	Text string
}

func findHeadingIDs(content, heading string) ([]string, error) {
	decoder := xml.NewDecoder(strings.NewReader(content))
	var ids []string
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return ids, nil
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "h1" {
			continue
		}
		var text string
		if err := decoder.DecodeElement(&text, &start); err != nil {
			return nil, err
		}
		if strings.TrimSpace(text) != heading {
			continue
		}
		for _, attr := range start.Attr {
			if attr.Name.Local == "id" && attr.Value != "" {
				ids = append(ids, attr.Value)
			}
		}
	}
}

func parseTopLevelBlocks(content string) ([]parsedBlock, error) {
	decoder := xml.NewDecoder(strings.NewReader(content))
	depth := 0
	inFragment := false
	var blocks []parsedBlock
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return blocks, nil
		}
		if err != nil {
			return nil, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			if !inFragment && value.Name.Local == "fragment" {
				inFragment = true
				depth = 0
				continue
			}
			if inFragment && depth == 0 {
				var inner struct {
					Text string `xml:",innerxml"`
				}
				id := ""
				for _, attr := range value.Attr {
					if attr.Name.Local == "id" {
						id = attr.Value
					}
				}
				if err := decoder.DecodeElement(&inner, &value); err != nil {
					return nil, err
				}
				blocks = append(blocks, parsedBlock{ID: id, Name: value.Name.Local, Text: stripXML(inner.Text)})
				continue
			}
			if inFragment {
				depth++
			}
		case xml.EndElement:
			if inFragment && value.Name.Local == "fragment" && depth == 0 {
				inFragment = false
				continue
			}
			if inFragment && depth > 0 {
				depth--
			}
		}
	}
}

func stripXML(value string) string {
	decoder := xml.NewDecoder(strings.NewReader("<root>" + value + "</root>"))
	var b strings.Builder
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		if chars, ok := token.(xml.CharData); ok {
			b.Write(chars)
		}
	}
	return strings.TrimSpace(b.String())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
