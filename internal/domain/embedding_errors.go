package domain

import "errors"

// ErrEmbeddingInputRejected identifies a provider rejection that can be isolated to smaller inputs.
var ErrEmbeddingInputRejected = errors.New("embedding input rejected")

// ErrEmbeddingBatchLimit identifies a request exceeding the client's bounded batch envelope.
var ErrEmbeddingBatchLimit = errors.New("embedding batch exceeds client limits")
