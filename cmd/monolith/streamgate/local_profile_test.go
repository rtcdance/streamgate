package main

import (
	"testing"

	"github.com/rtcdance/streamgate/test/testutil/profileguard"
)

func TestLocalSepoliaProfilePassesConfigGuard(t *testing.T) {
	profileguard.PassConfigGuard(t, "monolith", main)
}
