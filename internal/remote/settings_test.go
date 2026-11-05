package remote

import (
	"os"
	"strings"
	"testing"
)

func TestSettingsValidateS3(t *testing.T) {
	base := Settings{
		Enabled: true, Provider: ProviderS3,
		Bucket: "notes", AccessKey: "a", SecretKey: "s",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid settings: %v", err)
	}
	missing := base
	missing.AccessKey = ""
	if err := missing.Validate(); err == nil || !strings.Contains(err.Error(), "Access Key") {
		t.Fatalf("missing key: %v", err)
	}
	badPrefix := base
	badPrefix.Prefix = "a/../b"
	if err := badPrefix.Validate(); err == nil {
		t.Fatal("a prefix climbing out of the bucket was accepted")
	}
	badEndpoint := base
	badEndpoint.Endpoint = "ftp://example.com"
	if err := badEndpoint.Validate(); err == nil {
		t.Fatal("an ftp endpoint was accepted")
	}
}

func TestSettingsValidateMount(t *testing.T) {
	dir := t.TempDir()
	good := Settings{Enabled: true, Provider: ProviderNFS, MountPath: dir}
	if err := good.Validate(); err != nil {
		t.Fatalf("valid mount: %v", err)
	}
	missing := Settings{Enabled: true, Provider: ProviderSMB, MountPath: dir + "/nope"}
	if err := missing.Validate(); err == nil {
		t.Fatal("a missing share was accepted")
	}
	file := dir + "/file"
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	notDir := Settings{Enabled: true, Provider: ProviderNFS, MountPath: file}
	if err := notDir.Validate(); err == nil {
		t.Fatal("a file as a share was accepted")
	}
}

// Changing the region changes the Amazon endpoint, and so the remote the
// manifest belongs to.
func TestSettingsTargetFollowsRegion(t *testing.T) {
	settings := Settings{Enabled: true, Provider: ProviderS3, Bucket: "notes"}
	east := settings.WithDefaults()
	west := settings.WithDefaults()
	west.Region = "eu-west-1"
	if east.Target() == west.Target() {
		t.Fatalf("targets are both %q", east.Target())
	}
	if !strings.Contains(west.Target(), "eu-west-1") {
		t.Fatalf("target %q does not name the region endpoint", west.Target())
	}
}

// Colors of the mount path are trimmed, and a prefix's slashes go.
func TestSettingsNormalized(t *testing.T) {
	s := Settings{
		Enabled: true, Provider: ProviderS3, Bucket: " notes ",
		Prefix: "/vault/", MountPath: "  " + t.TempDir() + "  ",
	}
	n := s.Normalized()
	if n.Bucket != "notes" || n.Prefix != "vault" {
		t.Fatalf("normalized: %+v", n)
	}
	if strings.HasSuffix(n.MountPath, " ") {
		t.Fatalf("mount path keeps spaces: %q", n.MountPath)
	}
}
