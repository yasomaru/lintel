package sitter

import (
	"context"
	"fmt"
)

type (
	Language struct {
		t TreeSitter
		l uint64
	}

	LanguageError struct {
		version uint64
	}
)

func (l LanguageError) Error() string {
	return fmt.Sprintf("Incompatible language version %d", l.version)
}

func NewLanguage(l uint64, t TreeSitter) Language {
	return Language{l: l, t: t}
}

// Language returns a bundled grammar by tree-sitter name, e.g.
// "typescript", "tsx", "javascript", "go", "python", "java".
func (t TreeSitter) Language(ctx context.Context, name string) (Language, error) {
	fn := t.m.ExportedFunction("tree_sitter_" + name)
	if fn == nil {
		return Language{}, fmt.Errorf("grammar %q is not bundled", name)
	}
	ptr, err := fn.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating %s language: %w", name, err)
	}
	return NewLanguage(ptr[0], t), nil
}
