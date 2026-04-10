// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"testing"
)

func TestBannerText(t *testing.T) {
	label := bannerText()
	if label != "clictl" {
		t.Errorf("expected 'clictl', got %q", label)
	}
}
