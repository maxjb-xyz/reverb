package lyrics

import "testing"

func TestParse_SyncedBasic(t *testing.T) {
	got := Parse("[00:12.34]Hello world\n[01:02.5]Second line")
	if !got.Synced || len(got.Lines) != 2 {
		t.Fatalf("want 2 synced lines, got %+v", got)
	}
	if got.Lines[0].TimeMs != 12340 || got.Lines[0].Text != "Hello world" {
		t.Fatalf("line 0 = %+v", got.Lines[0])
	}
	if got.Lines[1].TimeMs != 62500 {
		t.Fatalf("line 1 time = %d, want 62500", got.Lines[1].TimeMs)
	}
}

func TestParse_MultipleTimestampsPerLine(t *testing.T) {
	got := Parse("[00:10.00][00:20.00]Chorus")
	if len(got.Lines) != 2 || got.Lines[0].TimeMs != 10000 || got.Lines[1].TimeMs != 20000 {
		t.Fatalf("want chorus at 10s and 20s, got %+v", got.Lines)
	}
	if got.Lines[0].Text != "Chorus" || got.Lines[1].Text != "Chorus" {
		t.Fatalf("both lines must carry the text: %+v", got.Lines)
	}
}

func TestParse_SkipsMetadataTags(t *testing.T) {
	got := Parse("[ar:Artist]\n[ti:Title]\n[00:01.00]Go")
	if len(got.Lines) != 1 || got.Lines[0].Text != "Go" {
		t.Fatalf("metadata tags must be dropped, got %+v", got.Lines)
	}
}

func TestParse_StripsEnhancedWordTimestamps(t *testing.T) {
	got := Parse("[00:01.00]<00:01.00>Hello <00:01.50>there")
	if got.Lines[0].Text != "Hello there" {
		t.Fatalf("enhanced stamps must be stripped, got %q", got.Lines[0].Text)
	}
}

func TestParse_SortsByTime(t *testing.T) {
	got := Parse("[00:20.00]B\n[00:10.00]A")
	if got.Lines[0].Text != "A" || got.Lines[1].Text != "B" {
		t.Fatalf("lines must be time-sorted, got %+v", got.Lines)
	}
}

func TestParse_KeepsBlankTimedLinesAsGaps(t *testing.T) {
	got := Parse("[00:01.00]Verse\n[00:05.00]\n[00:09.00]Next")
	if len(got.Lines) != 3 || got.Lines[1].Text != "" {
		t.Fatalf("blank timed line is an instrumental gap, got %+v", got.Lines)
	}
}

func TestParse_PlainTextFallback(t *testing.T) {
	got := Parse("Just some lyrics\nwith no timestamps")
	if got.Synced || got.Plain != "Just some lyrics\nwith no timestamps" {
		t.Fatalf("untimed text must fall back to plain, got %+v", got)
	}
}

func TestParse_EmptyInput(t *testing.T) {
	got := Parse("  \n \n")
	if got.Synced || got.Plain != "" || len(got.Lines) != 0 {
		t.Fatalf("empty input must be zero value, got %+v", got)
	}
}
