package util

import (
	"fmt"
	"os"
	"path/filepath"
)

// FileExists checks if file exists
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// DirExists checks if directory exists
func DirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// CreateDir creates directory
func CreateDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// ReadFile reads file content
func ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// WriteFile writes content to file
func WriteFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}

// DeleteFile deletes file
func DeleteFile(path string) error {
	return os.Remove(path)
}

// GetFileSize returns file size
func GetFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// GetFileExtension returns file extension
func GetFileExtension(path string) string {
	return filepath.Ext(path)
}

// GetFileName returns file name without extension
func GetFileName(path string) string {
	return filepath.Base(path)
}

// GetFileDir returns directory of file
func GetFileDir(path string) string {
	return filepath.Dir(path)
}

// JoinPath joins path components
func JoinPath(components ...string) string {
	return filepath.Join(components...)
}

// ListFiles lists files in directory
func ListFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, entry.Name())
		}
	}
	return files, nil
}

// ListDirs lists directories in directory
func ListDirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}
	return dirs, nil
}

// CopyFile copies file from src to dst
func CopyFile(src, dst string) error {
	data, err := ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read source file: %w", err)
	}

	err = WriteFile(dst, data)
	if err != nil {
		return fmt.Errorf("failed to write destination file: %w", err)
	}

	return nil
}
