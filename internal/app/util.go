package app

import (
	"time"

	"github.com/culmat/luanthetti/internal/fsutil"
)

func nowStamp() string { return time.Now().UTC().Format("20060102T150405") }

func moveDir(src, dst string) error { return fsutil.MoveDir(src, dst) }
