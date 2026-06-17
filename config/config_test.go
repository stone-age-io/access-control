package config

import "testing"

// A missing config file is fine; subjects.root defaults to "acc".
func TestLoadSubjectsRootDefault(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Subjects.Root != "acc" {
		t.Errorf("default subjects.root = %q, want acc", cfg.Subjects.Root)
	}
}

func TestLoadSubjectsRootEnvOverride(t *testing.T) {
	t.Setenv("SA_SUBJECTS_ROOT", "pacs")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Subjects.Root != "pacs" {
		t.Errorf("subjects.root = %q, want pacs", cfg.Subjects.Root)
	}
}

// A root that isn't a single NATS token must be rejected — parsing compares it
// against the first subject segment, so a dot/wildcard/space breaks routing.
func TestLoadSubjectsRootInvalid(t *testing.T) {
	for _, bad := range []string{"a.b", "ac*", "ac>", "a b"} {
		t.Setenv("SA_SUBJECTS_ROOT", bad)
		if _, err := Load(""); err == nil {
			t.Errorf("Load with subjects.root=%q: want error, got nil", bad)
		}
	}
}
