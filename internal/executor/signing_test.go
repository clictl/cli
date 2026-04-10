// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package executor

import (
	"net/http"
	"testing"
)

func TestSignRequest_NoOp(t *testing.T) {
	headers := http.Header{}
	err := SignRequest("POST", "https://api.example.com/data", headers, []byte(`{"query": "test"}`))
	if err != nil {
		t.Fatalf("SignRequest should be a no-op, got: %v", err)
	}
}

func TestSignRequest_NilBody(t *testing.T) {
	headers := http.Header{}
	err := SignRequest("GET", "https://example.com", headers, nil)
	if err != nil {
		t.Fatalf("SignRequest nil body should be a no-op, got: %v", err)
	}
}
