package main

import (
	"errors"
	"strings"
)

// Ошибки уровня хранилища. API переводит их в HTTP-коды.
var (
	ErrNetworkExists   = errors.New("сеть с таким именем уже существует")
	ErrNetworkNotFound = errors.New("сеть не найдена")
	ErrNodeNotFound    = errors.New("узел не найден")
	ErrNodeNameTaken   = errors.New("имя узла уже занято в этой сети")
	ErrAddressTaken    = errors.New("адрес уже занят в этой сети")
	ErrSubnetExhausted = errors.New("исчерпан пул подсетей")
	ErrNetworkFull     = errors.New("в сети достигнут предел участников")
)

// equalFold сравнивает имена сетей и узлов без учёта регистра.
func equalFold(a, b string) bool {
	return strings.EqualFold(a, b)
}
