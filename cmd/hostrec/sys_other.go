//go:build !darwin && !linux

package main

import (
	"errors"
	"os"
	"time"
)

func born(info os.FileInfo) time.Time { return info.ModTime() }

func parent(int) (parentProc, error) { return parentProc{}, errors.New("not on this system") }
