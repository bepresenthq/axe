package preview

// runbp owns the source mirror. It changes that mirror only between requests;
// this loop deliberately has no file watcher. A result is emitted only after
// the compiler/deployer and the native main-thread layout barrier finish.
import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/k-kohey/axe/internal/preview/analysis"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/k-kohey/axe/internal/preview/build"
)

func init() {
	if len(os.Args) == 2 && os.Args[1] == "runbp-device-gate" {
		fmt.Println("runbp.preview.device-gate.v1")
		os.Exit(0)
	}
	if len(os.Args) == 2 && os.Args[1] == "runbp-protocol" {
		fmt.Println("runbp.preview.control.v1")
		os.Exit(0)
	}
}

type runbpRequest struct {
	ID        string   `json:"id"`
	Revision  string   `json:"revision"`
	Selector  string   `json:"selector"`
	Changed   []string `json:"changed"`
	Operation string   `json:"operation"`
}
type runbpFixture struct {
	Selector string `json:"selector"`
	Title    string `json:"title"`
}
type runbpResult struct {
	Fixtures []runbpFixture `json:"fixtures,omitempty"`
	ID       string         `json:"id"`
	Revision string         `json:"revision"`
	Status   string         `json:"status"`
	Mode     string         `json:"mode"`
	Error    string         `json:"error,omitempty"`
}

func runbpBarrier(ctx context.Context, socket string) error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c, err := net.DialTimeout("unix", socket, time.Second)
		if err == nil {
			c.SetDeadline(time.Now().Add(2 * time.Second))
			_, err = fmt.Fprintln(c, "RUNBP_BARRIER")
			if err == nil {
				var line string
				line, err = bufio.NewReader(c).ReadString('\n')
				if line != "OK\n" {
					err = fmt.Errorf("native preview is not ready: %s", line)
				}
			}
			c.Close()
			if err == nil {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("native preview did not acknowledge its layout")
}

func runbpRespond(dir, source string, req runbpRequest, mode string, err error) error {
	result := runbpResult{ID: req.ID, Revision: req.Revision, Status: "rendered", Mode: mode}
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
	}
	if err == nil {
		if blocks, parseErr := analysis.PreviewBlocks(source); parseErr == nil {
			for index, block := range blocks {
				result.Fixtures = append(result.Fixtures, runbpFixture{Selector: strconv.Itoa(index), Title: block.Title})
			}
		}
	}
	bytes, e := json.Marshal(result)
	if e != nil {
		return e
	}
	temp := filepath.Join(dir, "result.tmp")
	if e = os.WriteFile(temp, bytes, 0600); e != nil {
		return e
	}
	return os.Rename(temp, filepath.Join(dir, "result.json"))
}

func runbpRead(dir string) (runbpRequest, error) {
	var req runbpRequest
	bytes, err := os.ReadFile(filepath.Join(dir, "request.json"))
	if err != nil {
		return req, err
	}
	err = json.Unmarshal(bytes, &req)
	if err == nil && (req.ID == "" || req.Revision == "") {
		err = fmt.Errorf("request requires ID and revision")
	}
	return req, err
}

func runbpControlledWatcher(ctx context.Context, dir, source string, pc ProjectConfig, bs *build.Settings, dirs previewDirs, wctx watchContext, ws *watchState) error {
	req, err := runbpRead(dir)
	if err != nil {
		return err
	}
	if err = runbpRespond(dir, source, req, "initial", runbpBarrier(ctx, dirs.Socket)); err != nil {
		return err
	}
	last := req.ID
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			req, err = runbpRead(dir)
			if err != nil || req.ID == last {
				continue
			}
			last = req.ID
			mode := "hot-reload"
			ws.mu.Lock()
			ws.previewSelector = req.Selector
			tracked := buildTrackedSet(ws.trackedFiles)
			ws.mu.Unlock()
			rebuild := false
			for _, rel := range req.Changed {
				file := filepath.Clean(filepath.Join(os.Getenv("RUNBP_SOURCE_ROOT"), rel))
				if _, ok := tracked[file]; !ok {
					rebuild = true
					break
				}
				ws.mu.Lock()
				prev := ws.skeletonMap[file]
				ws.mu.Unlock()
				strategy, _ := classifyChange(file, prev)
				if strategy == strategyRebuild {
					rebuild = true
					break
				}
			}
			if rebuild {
				mode = "rebuild"
				err = rebuildAndRelaunch(ctx, source, pc, bs, dirs, wctx, ws)
			} else {
				err = reloadMultiFile(ctx, source, bs, dirs, wctx, ws)
			}
			if err == nil {
				refreshTrackedState(ws)
				err = runbpBarrier(ctx, dirs.Socket)
			}
			if e := runbpRespond(dir, source, req, mode, err); e != nil {
				return e
			}
		}
	}
}

// Compilation may overlap runbp's boot/profile preparation. Do not touch the
// device until runbp has finished its final boot and required-service checks.
func runbpWaitForDevice(ctx context.Context, marker, device string) error {
	if marker == "" {
		return nil
	}
	fmt.Fprintln(os.Stderr, "[runbp] Waiting for simulator preparation")
	for {
		data, err := os.ReadFile(marker)
		if err == nil {
			var ready struct {
				UDID string `json:"udid"`
			}
			if json.Unmarshal(data, &ready) != nil || ready.UDID != device {
				return fmt.Errorf("preview simulator readiness marker does not match %s", device)
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("preview simulator readiness: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
