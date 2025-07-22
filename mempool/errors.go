package mempool

import "errors"

// Error definitions
var (
	ErrNoMessages         = errors.New("transaction has no messages")
	ErrExpectedOneMessage = errors.New("expected 1 message")
	ErrExpectedOneError   = errors.New("expected 1 error")
	ErrNotEVMTransaction  = errors.New("transaction is not an EVM transaction")
)
