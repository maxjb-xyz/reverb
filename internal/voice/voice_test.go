package voice

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckText(t *testing.T) {
	if err := CheckText("Up next, some Bonobo."); err != nil {
		t.Fatalf("ok text: %v", err)
	}
	if err := CheckText(" \n\t"); !errors.Is(err, ErrEmptyText) {
		t.Fatalf("blank: %v", err)
	}
	if err := CheckText(strings.Repeat("é", MaxTextLen)); err != nil {
		t.Fatalf("max length in runes should pass: %v", err)
	}
	if err := CheckText(strings.Repeat("é", MaxTextLen+1)); !errors.Is(err, ErrTextTooLong) {
		t.Fatalf("too long: %v", err)
	}
}
