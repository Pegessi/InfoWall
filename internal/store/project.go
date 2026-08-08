package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/infowall/infowall/internal/model"
)

type ProjectUpdate struct {
	Name        *string
	Description *string
	Color       *string
	Status      *model.ProjectStatus
}

func (u ProjectUpdate) Empty() bool {
	return u.Name == nil && u.Description == nil && u.Color == nil && u.Status == nil
}

func (s *Store) CreateProject(ctx context.Context, project *model.Project) (*model.Project, error) {
	if project == nil {
		return nil, fmt.Errorf("project is required")
	}
	p := *project
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return nil, fmt.Errorf("project name is required")
	}
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	if p.Status == "" {
		p.Status = model.ProjectStatusActive
	}
	if !p.Status.Valid() {
		return nil, fmt.Errorf("invalid project status %q", p.Status)
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects
		(id, name, description, color, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, p.ID, p.Name, p.Description, p.Color,
		p.Status, p.CreatedAt.UTC(), p.UpdatedAt); err != nil {
		if isUniqueConstraint(err) {
			return nil, fmt.Errorf("%w: project id or name already exists", ErrConflict)
		}
		return nil, fmt.Errorf("insert project: %w", err)
	}
	if err := markFeishuDirtyTx(ctx, tx, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) ListProjects(ctx context.Context) ([]*model.Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, description, color, status, created_at, updated_at
		FROM projects ORDER BY CASE status WHEN 'active' THEN 0 ELSE 1 END, name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := make([]*model.Project, 0)
	for rows.Next() {
		project, err := scanProject(rows.Scan)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

func (s *Store) GetProject(ctx context.Context, id string) (*model.Project, error) {
	return scanProject(s.db.QueryRowContext(ctx, `SELECT id, name, description, color, status, created_at, updated_at
		FROM projects WHERE id = ?`, id).Scan)
}

func scanProject(scan func(...any) error) (*model.Project, error) {
	var project model.Project
	var createdRaw, updatedRaw any
	if err := scan(&project.ID, &project.Name, &project.Description, &project.Color,
		&project.Status, &createdRaw, &updatedRaw); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	project.CreatedAt = parseSQLiteTime(createdRaw)
	project.UpdatedAt = parseSQLiteTime(updatedRaw)
	return &project, nil
}

func (s *Store) UpdateProject(ctx context.Context, id string, update ProjectUpdate) (*model.Project, error) {
	if update.Empty() {
		return nil, fmt.Errorf("project patch has no fields")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	project, err := scanProject(tx.QueryRowContext(ctx, `SELECT id, name, description, color, status, created_at, updated_at
		FROM projects WHERE id = ?`, id).Scan)
	if err != nil {
		return nil, err
	}
	if update.Name != nil {
		project.Name = strings.TrimSpace(*update.Name)
		if project.Name == "" {
			return nil, fmt.Errorf("project name is required")
		}
	}
	if update.Description != nil {
		project.Description = *update.Description
	}
	if update.Color != nil {
		project.Color = *update.Color
	}
	if update.Status != nil {
		if !update.Status.Valid() {
			return nil, fmt.Errorf("invalid project status %q", *update.Status)
		}
		project.Status = *update.Status
	}
	project.UpdatedAt = time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET name = ?, description = ?, color = ?, status = ?, updated_at = ? WHERE id = ?`,
		project.Name, project.Description, project.Color, project.Status, project.UpdatedAt, id); err != nil {
		if isUniqueConstraint(err) {
			return nil, fmt.Errorf("%w: project name already exists", ErrConflict)
		}
		return nil, fmt.Errorf("update project: %w", err)
	}
	if err := markFeishuDirtyTx(ctx, tx, project.UpdatedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return project, nil
}

func resolveProjectTx(ctx context.Context, tx *sql.Tx, candidate *string, hint string) (*string, string, error) {
	if candidate == nil || strings.TrimSpace(*candidate) == "" {
		return nil, strings.TrimSpace(hint), nil
	}
	value := strings.TrimSpace(*candidate)
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM projects WHERE id = ? OR name = ? COLLATE NOCASE LIMIT 1`, value, value).Scan(&id)
	if err == nil {
		return &id, strings.TrimSpace(hint), nil
	}
	if err != sql.ErrNoRows {
		return nil, hint, err
	}
	if strings.TrimSpace(hint) == "" {
		hint = value
	}
	return nil, strings.TrimSpace(hint), nil
}
