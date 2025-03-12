package model

import (
	"encoding/json"
	"errors"
	"log"
)

type Validated interface {
	IsValid() bool
}

func ParseAndValidate[T Validated](rawJSON string) (*T, error) {
	var data T
	if err := json.Unmarshal([]byte(rawJSON), &data); err != nil {
		log.Printf("Unmarshal error: %s", err.Error())
		return nil, err
	}
	if !data.IsValid() {
		return nil, errors.New("invalid data")
	}
	return &data, nil
}
