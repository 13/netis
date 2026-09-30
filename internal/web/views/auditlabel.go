package views

import "strconv"

// auditActionLabels name the audit log's actions for people. The log stores
// the short action names (auditActions in the web package); the page shows
// these, and a name missing here (a route audited under its pattern) shows
// as it was recorded.
var auditActionLabels = map[string]string{
	"login":                 "Signed in",
	"logout":                "Signed out",
	"setup":                 "Set up netis",
	"welcome.subnets":       "Chose subnets in setup",
	"welcome.integrations":  "Set up integrations in setup",
	"welcome.skip":          "Skipped setup",
	"welcome.dismiss":       "Closed the setup panel",
	"scan.all":              "Scanned all subnets",
	"scan.subnet":           "Scanned a subnet",
	"ip.kind":               "Changed an address lease",
	"device.create":         "Added a device",
	"device.update":         "Edited a device",
	"device.delete":         "Deleted a device",
	"device.approve":        "Approved a device",
	"device.bulk_approve":   "Approved devices",
	"device.bulk_tag":       "Tagged devices",
	"device.bulk_delete":    "Deleted devices",
	"device.wol":            "Sent Wake on LAN",
	"device.portscan":       "Scanned ports",
	"device.alert":          "Changed offline alerts",
	"device.import":         "Imported devices",
	"link.add":              "Added a link",
	"link.delete":           "Deleted a link",
	"field.set":             "Saved a custom field",
	"field.delete":          "Deleted a custom field",
	"notifications.save":    "Saved notifications",
	"notifications.test":    "Sent a test notification",
	"subnet.create":         "Added a subnet",
	"subnet.update":         "Edited a subnet",
	"subnet.delete":         "Deleted a subnet",
	"integrations.save":     "Saved integrations",
	"integration.run":       "Ran an integration",
	"tag.color":             "Changed a tag colour",
	"tag.rename":            "Renamed a tag",
	"tag.delete":            "Deleted a tag",
	"user.create":           "Added a user",
	"user.delete":           "Deleted a user",
	"user.role":             "Changed a role",
	"user.password_reset":   "Reset a password",
	"password.change":       "Changed own password",
	"session.revoke":        "Signed out a session",
	"session.revoke_others": "Signed out other sessions",
	"settings.save":         "Saved settings",
	"token.create":          "Created an API token",
	"token.revoke":          "Revoked an API token",
	"sso.link_start":        "Linked SSO",
	"api.device.create":     "Added a device (API)",
	"api.device.update":     "Edited a device (API)",
	"api.device.delete":     "Deleted a device (API)",
}

// AuditActionLabel names an audit action for people.
func AuditActionLabel(action string) string {
	if l, ok := auditActionLabels[action]; ok {
		return l
	}
	return action
}

// auditOutcome says in a word how a recorded request ended: done, refused
// (not signed in, not allowed, or throttled) or failed. The status code
// stays in the tooltip for whoever needs it.
func auditOutcome(status int) (word, tone string) {
	switch {
	case status == 401 || status == 403 || status == 429:
		return "Refused", "is-error"
	case status >= 400:
		return "Failed", "is-error"
	}
	return "Done", ""
}

func statusTitle(status int) string { return "HTTP status " + strconv.Itoa(status) }
