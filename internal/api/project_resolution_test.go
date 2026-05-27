package api

import "testing"

func TestProjectIDFromEnv(t *testing.T) {
	tests := []struct {
		name       string
		projectID  string
		googleProj string
		want       string
	}{
		{"prefers PROJECT_ID", "proj-a", "proj-b", "proj-a"},
		{"falls back to GOOGLE_CLOUD_PROJECT", "", "proj-b", "proj-b"},
		{"empty when neither set", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PROJECT_ID", tt.projectID)
			t.Setenv("GOOGLE_CLOUD_PROJECT", tt.googleProj)
			if got := projectIDFromEnv(); got != tt.want {
				t.Errorf("projectIDFromEnv() = %q, want %q", got, tt.want)
			}
		})
	}
}
