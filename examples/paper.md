---
type: paper
title: Attention Is All You Need
authors:
  - Ashish Vaswani
  - Noam Shazeer
  - Niki Parmar
  - Jakob Uszkoreit
  - Llion Jones
  - Aidan N. Gomez
  - Łukasz Kaiser
  - Illia Polosukhin
published: 2017-06-12
url: https://arxiv.org/abs/1706.03762
venue: NeurIPS 2017
tags: [ml, transformer, classic]
---

The dominant sequence transduction models at the time were based on complex
recurrent or convolutional neural networks that include an encoder and a
decoder connected through an attention mechanism. This paper proposes the
**Transformer**, a model architecture eschewing recurrence and convolutions
entirely, based solely on attention mechanisms.

Key results:

- **WMT 2014 English-to-German translation**: 28.4 BLEU, surpassing the best
  previously reported results (including ensembles) by over 2 BLEU.
- **WMT 2014 English-to-French translation**: 41.0 BLEU after training for
  3.5 days on 8 GPUs — a small fraction of the training cost of prior
  state-of-the-art models.
- Scales well to English constituency parsing, both with and without
  large-scale training data.

Why it matters: self-attention directly models relationships between all
positions in a sequence, enabling much more parallelism during training and
removing the sequential bottleneck of RNNs. The Transformer is the
architectural foundation of BERT, GPT, T5, and essentially every modern LLM.
