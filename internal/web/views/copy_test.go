package views

import "testing"

func TestSentence(t *testing.T) {
	for in, want := range map[string]string{
		"":                            "",
		"invalid MAC address":         "Invalid MAC address",
		"Already capitalised":         "Already capitalised",
		"élan":                        "Élan",
		"a subnet with that CIDR ...": "A subnet with that CIDR ...",
	} {
		if got := Sentence(in); got != want {
			t.Errorf("Sentence(%q) = %q, want %q", in, got, want)
		}
	}
}

// The audit log shows words, not action slugs or HTTP codes.
func TestAuditWords(t *testing.T) {
	if got := AuditActionLabel("device.bulk_delete"); got != "Deleted devices" {
		t.Errorf("label = %q", got)
	}
	if got := AuditActionLabel("POST /new/route"); got != "POST /new/route" {
		t.Errorf("unknown action should show as recorded, got %q", got)
	}
	for status, want := range map[int]string{303: "Done", 200: "Done", 401: "Refused", 403: "Refused", 429: "Refused", 400: "Failed", 500: "Failed"} {
		if got, _ := auditOutcome(status); got != want {
			t.Errorf("auditOutcome(%d) = %q, want %q", status, got, want)
		}
	}
}

func TestRoleLabel(t *testing.T) {
	if roleLabel("admin") != "Admin" || roleLabel("viewer") != "Viewer" {
		t.Error("roles should read Admin and Viewer")
	}
}
