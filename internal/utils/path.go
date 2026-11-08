package utils

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// LogRoot is the only directory the panel reads core logs from.
const LogRoot = "/opt/var/log"

// PathValidator validates file paths against allowed root directories to prevent path traversal attacks.
type PathValidator struct {
	AllowedRoots []string
}

// NewPathValidator creates a new PathValidator instance with the specified allowed root paths.
func NewPathValidator(roots []string) *PathValidator {
	return &PathValidator{AllowedRoots: roots}
}

// Validate resolves symlinks and cleans the input path, checking if it resides within the allowed roots.
func (v *PathValidator) Validate(path string) (string, error) {
	if path == "" {
		return "", errors.New("path traversal detected or path not allowed")
	}

	// Strict validation against path traversal and characters to satisfy static analyzers (CWE-22)
	if strings.Contains(path, "..") {
		return "", errors.New("path traversal detected or path not allowed")
	}
	if matched, _ := regexp.MatchString(`^[a-zA-Z0-9_\-\.\/]+$`, path); !matched {
		return "", errors.New("path traversal detected or path not allowed")
	}

	cleanPath := filepath.Clean(path)
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return "", errors.New("path traversal detected or path not allowed")
	}

	// Resolve symlinks for the target path. If it does not exist, resolve the
	// nearest existing ancestor and append the missing tail: missing components
	// cannot be symlinks. So a file in a directory that is yet to be created
	// (/opt/etc/mihomo/config.yaml before the first save) validates too.
	resolved, err := resolveExisting(absPath)
	if err != nil {
		return "", errors.New("path traversal detected or path not allowed")
	}

	for _, root := range v.AllowedRoots {
		cleanRoot := filepath.Clean(root)
		absRoot, err := filepath.Abs(cleanRoot)
		if err != nil {
			continue
		}

		resolvedRoot := absRoot
		if rr, err := filepath.EvalSymlinks(absRoot); err == nil {
			resolvedRoot = rr
		}

		rel, err := filepath.Rel(resolvedRoot, resolved)
		if err == nil && !strings.HasPrefix(rel, "..") && rel != ".." {
			return filepath.Join(resolvedRoot, rel), nil
		}
	}
	return "", errors.New("path traversal detected or path not allowed")
}

// RootNameValidator — узкая проверка одного имени файла в корне, закреплённом при
// создании (каталог конфигураций Xray). Нужна для файлов, имя которых не проходит
// латинскую регулярку Validate (кириллица, пробел, скобки): их видно, можно
// прочитать, удалить и переименовать в допустимое имя. Validate при этом не
// ослабляется, а вызывающий передаёт только имя и не может подставить другой
// каталог.
type RootNameValidator struct {
	root string
	base *PathValidator
}

// NewRootNameValidator создаёт проверку имён для корня root; сам корень обязан
// проходить base.Validate.
func NewRootNameValidator(root string, base *PathValidator) *RootNameValidator {
	return &RootNameValidator{root: root, base: base}
}

// rootNameRE — допустимые символы имени: буквы и цифры Unicode, пробел, точка,
// подчёркивание, дефис и круглые скобки.
var rootNameRE = regexp.MustCompile(`^[\p{L}\p{N} ._()\-]+$`)

// ValidateRootName проверяет одно имя файла в закреплённом корне и возвращает
// путь к нему. Имя — без разделителей, «..», NUL и обратной косой черты; корень
// проходит Validate; итоговый путь после разворачивания симлинков обязан лежать
// прямо в корне (родитель равен корню).
func (v *RootNameValidator) ValidateRootName(name string) (string, error) {
	errName := errors.New("file name not allowed")
	if v == nil || v.base == nil || name == "" || name == "." || filepath.Base(name) != name {
		return "", errName
	}
	if strings.Contains(name, "..") || strings.ContainsAny(name, "/\\\x00") {
		return "", errName
	}
	if !utf8.ValidString(name) || !rootNameRE.MatchString(name) {
		return "", errName
	}
	cleanRoot, err := v.base.Validate(v.root)
	if err != nil {
		return "", errName
	}
	joined := filepath.Join(cleanRoot, name)
	if filepath.Dir(joined) != cleanRoot {
		return "", errName
	}
	// codeql[go/path-injection] - name is a single path element (no separators, "..",
	// NUL or backslash, Unicode letters/digits only) joined to the fixed root that passed
	// PathValidator.Validate; the parent is re-checked against the root below.
	resolved, err := filepath.EvalSymlinks(joined)
	switch {
	case err == nil:
	case os.IsNotExist(err):
		resolved = joined
	default:
		return "", errName
	}
	if filepath.Dir(resolved) != cleanRoot {
		return "", errName
	}
	return resolved, nil
}

// ValidateUnder is Validate plus a check that the resolved path is root itself
// or lies under it (symlinks of both sides resolved), so an allowed root that is
// wider than root (a config directory next to the logs) does not let the path out.
func (v *PathValidator) ValidateUnder(path, root string) (string, error) {
	clean, err := v.Validate(path)
	if err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", errors.New("path traversal detected or path not allowed")
	}
	resolvedRoot, err := resolveExisting(absRoot)
	if err != nil {
		return "", errors.New("path traversal detected or path not allowed")
	}
	rel, err := filepath.Rel(resolvedRoot, clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path traversal detected or path not allowed")
	}
	return clean, nil
}

// resolveExisting resolves symlinks of the longest existing prefix of absPath
// and appends the rest unchanged.
func resolveExisting(absPath string) (string, error) {
	tail := ""
	cur := absPath
	for {
		if rp, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(rp, tail), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", os.ErrNotExist
		}
		tail = filepath.Join(filepath.Base(cur), tail)
		cur = parent
	}
}
