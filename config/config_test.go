package config

import "testing"

// A missing config file is fine; subjects.app defaults to "acc".
func TestLoadSubjectsAppDefault(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Subjects.App != "acc" {
		t.Errorf("default subjects.app = %q, want acc", cfg.Subjects.App)
	}
}

func TestLoadSubjectsAppEnvOverride(t *testing.T) {
	t.Setenv("SA_SUBJECTS_APP", "pacs")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Subjects.App != "pacs" {
		t.Errorf("subjects.app = %q, want pacs", cfg.Subjects.App)
	}
}

// An app token that isn't a single NATS token must be rejected — parsing
// compares it against a fixed subject segment, so a dot/wildcard/space breaks
// routing.
func TestLoadSubjectsAppInvalid(t *testing.T) {
	for _, bad := range []string{"a.b", "ac*", "ac>", "a b"} {
		t.Setenv("SA_SUBJECTS_APP", bad)
		if _, err := Load(""); err == nil {
			t.Errorf("Load with subjects.app=%q: want error, got nil", bad)
		}
	}
}

// Driver defaults to mock; the liveness durations default sensibly.
func TestControllerDriverDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Controller.Driver != "mock" {
		t.Errorf("default controller.driver = %q, want mock", cfg.Controller.Driver)
	}
	if cfg.Controller.HeartbeatInterval == 0 || cfg.Accessd.ControllerOfflineAfter == 0 {
		t.Errorf("liveness durations not defaulted: hb=%s offline=%s",
			cfg.Controller.HeartbeatInterval, cfg.Accessd.ControllerOfflineAfter)
	}
}

// driver=gpio requires a model; an unknown driver is rejected.
func TestControllerDriverValidation(t *testing.T) {
	t.Setenv("SA_CONTROLLER_DRIVER", "gpio")
	if _, err := Load(""); err == nil {
		t.Error("driver=gpio without model: want error, got nil")
	}
	t.Setenv("SA_CONTROLLER_MODEL", "kincony-server-mini")
	if _, err := Load(""); err != nil {
		t.Errorf("driver=gpio with model: unexpected error %v", err)
	}
	t.Setenv("SA_CONTROLLER_DRIVER", "bogus")
	if _, err := Load(""); err == nil {
		t.Error("driver=bogus: want error, got nil")
	}
}
