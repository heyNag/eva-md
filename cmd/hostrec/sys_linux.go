//go:build linux

package main

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

// born gives a file's creation time where the system keeps one; here its
// modification time.
func born(info os.FileInfo) time.Time { return info.ModTime() }

// parent reads a process from /proc: its name, its start in clock ticks
// after boot, and its arguments.
func parent(pid int) (parentProc, error) {
	var p parentProc
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return p, err
	}
	s := string(stat)
	open, end := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if open < 0 || end < open {
		return p, errors.New("process record does not read")
	}
	p.name = s[open+1 : end]
	fields := strings.Fields(s[end+1:])
	if len(fields) < 20 {
		return p, errors.New("process record does not read")
	}
	ticks, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return p, err
	}
	boot, err := bootTime()
	if err != nil {
		return p, err
	}
	p.born = boot.Add(time.Duration(ticks) * 10 * time.Millisecond)
	if raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline"); err == nil {
		p.login = loginArgs(strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00"))
	}
	return p, nil
}

func bootTime() (time.Time, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			sec, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return time.Time{}, err
			}
			return time.Unix(sec, 0), nil
		}
	}
	return time.Time{}, errors.New("no boot time")
}
