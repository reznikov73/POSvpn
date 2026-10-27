//go:build !windows

// Package clipboard — заглушка для не-Windows сборок.
package clipboard

import "errors"

// Write недоступен вне Windows.
func Write(string) error {
	return errors.New("буфер обмена доступен только в Windows")
}

// Read недоступен вне Windows.
func Read() (string, error) {
	return "", errors.New("буфер обмена доступен только в Windows")
}
