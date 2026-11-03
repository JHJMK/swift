package app

import "swift/internal/vault"

// The vault's types are named here so the app's views read as one package.
type (
	Node         = vault.Node
	Vault        = vault.Vault
	TrashEntry   = vault.TrashEntry
	SearchResult = vault.SearchResult
	RecentEntry  = vault.RecentEntry
)

// OpenVault opens the folder at root as a vault.
func OpenVault(root string) (*Vault, error) { return vault.OpenVault(root) }

// Small helpers of the vault the app's own files use unqualified.

func sanitizeName(s string) string { return vault.SanitizeName(s) }

func isMarkdown(name string) bool { return vault.IsMarkdown(name) }

func movePath(src, dst string) error { return vault.MovePath(src, dst) }

func writeFileAtomic(filename string, data []byte) error {
	return vault.WriteFileAtomic(filename, data)
}
