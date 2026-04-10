// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package transform

import (
	"testing"
)

func TestAssertStatus(t *testing.T) {
	a := Assertions{{Status: []int{200, 201}}}

	if err := a.Check(200, []byte(`{}`)); err != nil {
		t.Fatalf("200 should pass: %v", err)
	}
	if err := a.Check(201, []byte(`{}`)); err != nil {
		t.Fatalf("201 should pass: %v", err)
	}
	if err := a.Check(404, []byte(`{}`)); err == nil {
		t.Fatal("404 should fail")
	}
}

func TestAssertExists(t *testing.T) {
	body := []byte(`{"data": {"id": 1, "name": "test"}}`)
	a := Assertions{{Exists: "$.data.id"}}

	if err := a.Check(200, body); err != nil {
		t.Fatalf("should pass: %v", err)
	}

	a2 := Assertions{{Exists: "$.data.missing"}}
	if err := a2.Check(200, body); err == nil {
		t.Fatal("missing field should fail")
	}
}

func TestAssertNotEmpty(t *testing.T) {
	body := []byte(`{"name": "test", "empty": "", "items": []}`)

	if err := (Assertions{{NotEmpty: "$.name"}}).Check(200, body); err != nil {
		t.Fatalf("non-empty string should pass: %v", err)
	}
	if err := (Assertions{{NotEmpty: "$.empty"}}).Check(200, body); err == nil {
		t.Fatal("empty string should fail")
	}
	if err := (Assertions{{NotEmpty: "$.items"}}).Check(200, body); err == nil {
		t.Fatal("empty array should fail")
	}
}

func TestAssertEquals(t *testing.T) {
	body := []byte(`{"status": "ok", "count": 42}`)

	a := Assertions{{Equals: &EqualsAssertion{Path: "$.status", Value: "ok"}}}
	if err := a.Check(200, body); err != nil {
		t.Fatalf("should match: %v", err)
	}

	a2 := Assertions{{Equals: &EqualsAssertion{Path: "$.count", Value: 42}}}
	if err := a2.Check(200, body); err != nil {
		t.Fatalf("number should match: %v", err)
	}

	a3 := Assertions{{Equals: &EqualsAssertion{Path: "$.status", Value: "error"}}}
	if err := a3.Check(200, body); err == nil {
		t.Fatal("mismatch should fail")
	}
}

func TestAssertContains(t *testing.T) {
	body := []byte(`{"message": "hello world"}`)

	if err := (Assertions{{Contains: "hello"}}).Check(200, body); err != nil {
		t.Fatalf("should contain: %v", err)
	}
	if err := (Assertions{{Contains: "missing"}}).Check(200, body); err == nil {
		t.Fatal("missing substring should fail")
	}
}

func TestAssertJS(t *testing.T) {
	body := []byte(`{"data": [1, 2, 3]}`)

	a := Assertions{{JS: `function assert(resp) {
		if (resp.body.data.length !== 3) {
			return {pass: false, reason: "expected 3 items"};
		}
		return {pass: true};
	}`}}

	if err := a.Check(200, body); err != nil {
		t.Fatalf("should pass: %v", err)
	}

	a2 := Assertions{{JS: `function assert(resp) {
		if (resp.status_code !== 201) {
			return {pass: false, reason: "expected 201"};
		}
		return {pass: true};
	}`}}

	if err := a2.Check(200, body); err == nil {
		t.Fatal("status mismatch should fail")
	}
}

func TestAssertChain(t *testing.T) {
	body := []byte(`{"status": "ok", "data": {"id": 1}}`)

	a := Assertions{
		{Status: []int{200}},
		{Exists: "$.data.id"},
		{Equals: &EqualsAssertion{Path: "$.status", Value: "ok"}},
	}

	if err := a.Check(200, body); err != nil {
		t.Fatalf("all should pass: %v", err)
	}

	// First assertion fails, short-circuits
	if err := a.Check(500, body); err == nil {
		t.Fatal("status 500 should fail")
	}
}

func TestAssertEmpty(t *testing.T) {
	a := Assertions{}
	if err := a.Check(200, []byte(`{}`)); err != nil {
		t.Fatalf("empty assertions should pass: %v", err)
	}
}

func TestAssertNonJSON(t *testing.T) {
	body := []byte(`plain text response`)

	// Status works on non-JSON
	if err := (Assertions{{Status: []int{200}}}).Check(200, body); err != nil {
		t.Fatalf("status should work on non-JSON: %v", err)
	}

	// Contains works on non-JSON
	if err := (Assertions{{Contains: "plain text"}}).Check(200, body); err != nil {
		t.Fatalf("contains should work on non-JSON: %v", err)
	}

	// Exists requires JSON
	if err := (Assertions{{Exists: "$.data"}}).Check(200, body); err == nil {
		t.Fatal("exists on non-JSON should fail")
	}
}

func TestParseAssertions(t *testing.T) {
	raw := []any{
		map[string]any{"status": []any{float64(200), float64(201)}},
		map[string]any{"exists": "$.data"},
		map[string]any{"contains": "ok"},
	}
	assertions, err := ParseAssertions(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(assertions) != 3 {
		t.Fatalf("expected 3 assertions, got %d", len(assertions))
	}
	if len(assertions[0].Status) != 2 {
		t.Error("expected 2 status codes")
	}
}
