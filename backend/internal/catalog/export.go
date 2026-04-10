package catalog

import (
	"archive/zip"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// ExportZip writes all merged catalog entries as YAML files into a zip archive.
// The archive contains one file per rule named "<id>.yaml".
func (m *Manager) ExportZip(w io.Writer) error {
	zw := zip.NewWriter(w)
	defer zw.Close()

	for _, e := range m.All() {
		data, err := yaml.Marshal(e)
		if err != nil {
			return fmt.Errorf("marshal entry %s: %w", e.ID, err)
		}
		f, err := zw.Create(e.ID + ".yaml")
		if err != nil {
			return fmt.Errorf("create zip entry %s: %w", e.ID, err)
		}
		if _, err := f.Write(data); err != nil {
			return fmt.Errorf("write zip entry %s: %w", e.ID, err)
		}
	}
	return nil
}
