package util

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileExists(t *testing.T) {
	t.Run("existing file", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "test.txt")
		err := os.WriteFile(filePath, []byte("test"), 0644)
		require.NoError(t, err)

		assert.True(t, FileExists(filePath))
	})

	t.Run("non-existing file", func(t *testing.T) {
		assert.False(t, FileExists("/non/existing/file.txt"))
	})
}

func TestDirExists(t *testing.T) {
	t.Run("existing directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		assert.True(t, DirExists(tmpDir))
	})

	t.Run("non-existing directory", func(t *testing.T) {
		assert.False(t, DirExists("/non/existing/dir"))
	})

	t.Run("file is not directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "test.txt")
		err := os.WriteFile(filePath, []byte("test"), 0644)
		require.NoError(t, err)

		assert.False(t, DirExists(filePath))
	})
}

func TestCreateDir(t *testing.T) {
	t.Run("create new directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		newDir := filepath.Join(tmpDir, "newdir")

		err := CreateDir(newDir)
		require.NoError(t, err)
		assert.True(t, DirExists(newDir))
	})

	t.Run("create nested directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		nestedDir := filepath.Join(tmpDir, "a", "b", "c")

		err := CreateDir(nestedDir)
		require.NoError(t, err)
		assert.True(t, DirExists(nestedDir))
	})

	t.Run("create existing directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		err := CreateDir(tmpDir)
		assert.NoError(t, err)
	})
}

func TestReadFile(t *testing.T) {
	t.Run("read existing file", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "test.txt")
		expected := []byte("test content")
		err := os.WriteFile(filePath, expected, 0644)
		require.NoError(t, err)

		content, err := ReadFile(filePath)
		require.NoError(t, err)
		assert.Equal(t, expected, content)
	})

	t.Run("read non-existing file", func(t *testing.T) {
		_, err := ReadFile("/non/existing/file.txt")
		assert.Error(t, err)
	})
}

func TestWriteFile(t *testing.T) {
	t.Run("write new file", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "test.txt")
		data := []byte("test content")

		err := WriteFile(filePath, data)
		require.NoError(t, err)

		content, err := os.ReadFile(filePath)
		require.NoError(t, err)
		assert.Equal(t, data, content)
	})

	t.Run("overwrite existing file", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "test.txt")
		err := os.WriteFile(filePath, []byte("old content"), 0644)
		require.NoError(t, err)

		newData := []byte("new content")
		err = WriteFile(filePath, newData)
		require.NoError(t, err)

		content, err := os.ReadFile(filePath)
		require.NoError(t, err)
		assert.Equal(t, newData, content)
	})
}

func TestDeleteFile(t *testing.T) {
	t.Run("delete existing file", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "test.txt")
		err := os.WriteFile(filePath, []byte("test"), 0644)
		require.NoError(t, err)

		err = DeleteFile(filePath)
		require.NoError(t, err)
		assert.False(t, FileExists(filePath))
	})

	t.Run("delete non-existing file", func(t *testing.T) {
		err := DeleteFile("/non/existing/file.txt")
		assert.Error(t, err)
	})
}

func TestGetFileSize(t *testing.T) {
	t.Run("get size of existing file", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "test.txt")
		data := []byte("test content")
		err := os.WriteFile(filePath, data, 0644)
		require.NoError(t, err)

		size, err := GetFileSize(filePath)
		require.NoError(t, err)
		assert.Equal(t, int64(len(data)), size)
	})

	t.Run("get size of non-existing file", func(t *testing.T) {
		_, err := GetFileSize("/non/existing/file.txt")
		assert.Error(t, err)
	})
}

func TestFileSize(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.txt")
	data := []byte("test")
	err := os.WriteFile(filePath, data, 0644)
	require.NoError(t, err)

	size, err := FileSize(filePath)
	require.NoError(t, err)
	assert.Equal(t, int64(len(data)), size)
}

func TestGetFileExtension(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/path/to/file.txt", ".txt"},
		{"/path/to/file.tar.gz", ".gz"},
		{"/path/to/file", ""},
		{"/path/to/.hidden", ".hidden"},
		{"/path/to/.hidden.txt", ".txt"},
		{"/path/to/file.", "."},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, GetFileExtension(tt.input))
		})
	}
}

func TestGetFileName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/path/to/file.txt", "file.txt"},
		{"/path/to/file", "file"},
		{"file.txt", "file.txt"},
		{"/path/to/dir/", "dir"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, GetFileName(tt.input))
		})
	}
}

func TestGetFileDir(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/path/to/file.txt", "/path/to"},
		{"/path/to/file", "/path/to"},
		{"file.txt", "."},
		{"/path/to/dir/", "/path/to/dir"},
		{"/path/to/dir", "/path/to"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, GetFileDir(tt.input))
		})
	}
}

func TestJoinPath(t *testing.T) {
	tests := []struct {
		components []string
		want       string
	}{
		{[]string{"a", "b", "c"}, filepath.Join("a", "b", "c")},
		{[]string{"/a", "b", "c"}, filepath.Join("/a", "b", "c")},
		{[]string{}, ""},
		{[]string{"single"}, "single"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			assert.Equal(t, tt.want, JoinPath(tt.components...))
		})
	}
}

func TestListFiles(t *testing.T) {
	t.Run("list files in directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("test"), 0644)
		os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("test"), 0644)
		os.Mkdir(filepath.Join(tmpDir, "dir1"), 0755)

		files, err := ListFiles(tmpDir)
		require.NoError(t, err)
		assert.Len(t, files, 2)
		assert.Contains(t, files, "file1.txt")
		assert.Contains(t, files, "file2.txt")
	})

	t.Run("list files in empty directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		files, err := ListFiles(tmpDir)
		require.NoError(t, err)
		assert.Len(t, files, 0)
	})

	t.Run("list files in non-existing directory", func(t *testing.T) {
		_, err := ListFiles("/non/existing/dir")
		assert.Error(t, err)
	})
}

func TestListDirs(t *testing.T) {
	t.Run("list directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		os.Mkdir(filepath.Join(tmpDir, "dir1"), 0755)
		os.Mkdir(filepath.Join(tmpDir, "dir2"), 0755)
		os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("test"), 0644)

		dirs, err := ListDirs(tmpDir)
		require.NoError(t, err)
		assert.Len(t, dirs, 2)
		assert.Contains(t, dirs, "dir1")
		assert.Contains(t, dirs, "dir2")
	})

	t.Run("list directories in empty directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		dirs, err := ListDirs(tmpDir)
		require.NoError(t, err)
		assert.Len(t, dirs, 0)
	})
}

func TestCopyFile(t *testing.T) {
	t.Run("copy file successfully", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcPath := filepath.Join(tmpDir, "src.txt")
		dstPath := filepath.Join(tmpDir, "dst.txt")
		data := []byte("test content")
		err := os.WriteFile(srcPath, data, 0644)
		require.NoError(t, err)

		err = CopyFile(srcPath, dstPath)
		require.NoError(t, err)

		copied, err := os.ReadFile(dstPath)
		require.NoError(t, err)
		assert.Equal(t, data, copied)
	})

	t.Run("copy non-existing file", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcPath := filepath.Join(tmpDir, "nonexistent.txt")
		dstPath := filepath.Join(tmpDir, "dst.txt")

		err := CopyFile(srcPath, dstPath)
		assert.Error(t, err)
		assert.False(t, FileExists(dstPath))
	})
}
