// Package avoid speaks the boxz-avoid JSON protocol, an experimental
// orthogonal edge router backed by libavoid.
//
// The router runs as a separate process rather than as a linked library. That
// keeps libavoid's LGPL licence off the boxz binary, keeps C++ out of the Go
// build, and makes the router independently testable from a shell.
//
// Coordinates are SVG-style throughout: x grows east, y grows south. libavoid
// uses the same convention, so no axis conversion happens on either side.
package avoid

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// Version is the protocol version this package speaks. The router rejects a
// request carrying any other version.
const Version = 1

type Side string

const (
	North Side = "north"
	East  Side = "east"
	South Side = "south"
	West  Side = "west"
)

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Rect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// Port is a candidate attachment point on an obstacle's perimeter. Pos is the
// fraction along that side, west-to-east on north/south and north-to-south on
// east/west.
type Port struct {
	ID   string  `json:"id"`
	Side Side    `json:"side"`
	Pos  float64 `json:"pos"`
}

// Obstacle is a rectangle routes must avoid. An obstacle with no ports is a
// pure obstacle unless an edge names it, in which case edges attach to its
// centre and libavoid picks the departure side.
type Obstacle struct {
	ID    string `json:"id"`
	Rect  Rect   `json:"rect"`
	Ports []Port `json:"ports,omitempty"`
	// ExclusivePorts gives each port to at most one edge. Nil means the
	// router's default, which is exclusive.
	ExclusivePorts *bool `json:"exclusivePorts,omitempty"`
}

// Cluster is a penalised-crossing region rather than a hard obstacle.
//
// libavoid ignores clusters under orthogonal routing, so sending one currently
// only produces a warning. See avoidrouter/README.md.
type Cluster struct {
	ID   string `json:"id"`
	Rect Rect   `json:"rect"`
}

// Endpoint is one end of an edge, in one of three forms:
//
//   - Point set: a fixed coordinate, with Dirs restricting approach directions.
//   - Ports non-empty: any of that named subset of the obstacle's ports.
//   - neither: any of the obstacle's ports, or its centre if it declares none.
type Endpoint struct {
	Obstacle string   `json:"obstacle,omitempty"`
	Ports    []string `json:"ports,omitempty"`
	Point    *Point   `json:"point,omitempty"`
	Dirs     []Side   `json:"dirs,omitempty"`
}

type Edge struct {
	ID          string   `json:"id"`
	From        Endpoint `json:"from"`
	To          Endpoint `json:"to"`
	Checkpoints []Point  `json:"checkpoints,omitempty"`
}

// Options are passed through to libavoid. A nil field keeps the router's
// default, so callers only set what they mean to change.
type Options struct {
	// RoutingType is "orthogonal" (default) or "polyline".
	RoutingType             string   `json:"routingType,omitempty"`
	ShapeBufferDistance     *float64 `json:"shapeBufferDistance,omitempty"`
	IdealNudgingDistance    *float64 `json:"idealNudgingDistance,omitempty"`
	SegmentPenalty          *float64 `json:"segmentPenalty,omitempty"`
	AnglePenalty            *float64 `json:"anglePenalty,omitempty"`
	CrossingPenalty         *float64 `json:"crossingPenalty,omitempty"`
	ClusterCrossingPenalty  *float64 `json:"clusterCrossingPenalty,omitempty"`
	FixedSharedPathPenalty  *float64 `json:"fixedSharedPathPenalty,omitempty"`
	PortDirectionPenalty    *float64 `json:"portDirectionPenalty,omitempty"`
	ReverseDirectionPenalty *float64 `json:"reverseDirectionPenalty,omitempty"`

	NudgeOrthogonalSegmentsConnectedToShapes *bool `json:"nudgeOrthogonalSegmentsConnectedToShapes,omitempty"`
	NudgeSharedPathsWithCommonEndPoint       *bool `json:"nudgeSharedPathsWithCommonEndPoint,omitempty"`
	NudgeOrthogonalTouchingColinearSegments  *bool `json:"nudgeOrthogonalTouchingColinearSegments,omitempty"`
	PerformUnifyingNudgingPreprocessingStep  *bool `json:"performUnifyingNudgingPreprocessingStep,omitempty"`
	PenaliseOrthogonalSharedPathsAtConnEnds  *bool `json:"penaliseOrthogonalSharedPathsAtConnEnds,omitempty"`
	ImproveHyperedgeRoutesMovingJunctions    *bool `json:"improveHyperedgeRoutesMovingJunctions,omitempty"`
}

type Request struct {
	Version   int        `json:"version"`
	ID        string     `json:"id,omitempty"`
	Options   *Options   `json:"options,omitempty"`
	Obstacles []Obstacle `json:"obstacles,omitempty"`
	Clusters  []Cluster  `json:"clusters,omitempty"`
	Edges     []Edge     `json:"edges,omitempty"`
}

// ResolvedEnd reports where an edge actually attached, so boxz can render the
// port it did not choose itself.
type ResolvedEnd struct {
	Obstacle string `json:"obstacle,omitempty"`
	Port     string `json:"port,omitempty"`
	Side     Side   `json:"side,omitempty"`
	Point    Point  `json:"point"`
}

type Route struct {
	ID     string      `json:"id"`
	Points []Point     `json:"points"`
	From   ResolvedEnd `json:"from"`
	To     ResolvedEnd `json:"to"`
}

type Stats struct {
	ElapsedMs float64 `json:"elapsedMs"`
	Obstacles int     `json:"obstacles"`
	Edges     int     `json:"edges"`
}

type Response struct {
	Version  int      `json:"version"`
	ID       string   `json:"id,omitempty"`
	Routes   []Route  `json:"routes"`
	Warnings []string `json:"warnings,omitempty"`
	Stats    Stats    `json:"stats"`
	Error    *Error   `json:"error,omitempty"`
}

// Error is a request the router rejected. The process stays alive, so a
// rejected request does not invalidate a Client.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("boxz-avoid: %s: %s", e.Code, e.Message) }

// DefaultBinary is the executable name looked up on PATH.
const DefaultBinary = "boxz-avoid"

// BinaryEnv overrides binary discovery with an explicit path.
const BinaryEnv = "BOXZ_AVOID_BIN"

// FindBinary locates the router: the BOXZ_AVOID_BIN override first, then the
// in-tree CMake build directory, then PATH.
func FindBinary() (string, error) {
	if override := os.Getenv(BinaryEnv); override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", fmt.Errorf("avoid: %s=%s: %w", BinaryEnv, override, err)
		}
		return override, nil
	}
	for _, candidate := range []string{
		filepath.Join("avoidrouter", "build", DefaultBinary),
		filepath.Join("..", "..", "avoidrouter", "build", DefaultBinary),
	} {
		if absolute, err := filepath.Abs(candidate); err == nil {
			if _, err := os.Stat(absolute); err == nil {
				return absolute, nil
			}
		}
	}
	path, err := exec.LookPath(DefaultBinary)
	if err != nil {
		return "", fmt.Errorf("avoid: %s not found; build it with "+
			"cmake -S avoidrouter -B avoidrouter/build && cmake --build avoidrouter/build, "+
			"or set %s", DefaultBinary, BinaryEnv)
	}
	return path, nil
}

// Client is a long-lived router process. Requests are independent: the router
// keeps no state between them, so a Client is only an optimization that avoids
// paying process startup per render.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	mu     sync.Mutex
	closed bool
}

// Start launches the router. Pass an empty binary to use FindBinary. Router
// diagnostics (libavoid writes some directly to stderr) go to stderr, or are
// discarded when it is nil.
func Start(binary string, stderr io.Writer) (*Client, error) {
	if binary == "" {
		found, err := FindBinary()
		if err != nil {
			return nil, err
		}
		binary = found
	}
	if stderr == nil {
		stderr = io.Discard
	}
	cmd := exec.Command(binary)
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("avoid: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("avoid: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("avoid: start %s: %w", binary, err)
	}
	// The reader must be large enough for one whole response line; diagrams
	// with many edges produce long ones.
	return &Client{cmd: cmd, stdin: stdin, stdout: bufio.NewReaderSize(stdout, 1<<20)}, nil
}

// Route sends one request and returns its response. A request the router
// rejects comes back as an *Error, leaving the client usable.
func (c *Client) Route(request *Request) (*Response, error) {
	if request.Version == 0 {
		request.Version = Version
	}
	// The protocol is JSON Lines, so the encoded request must contain no
	// newline. json.Marshal never emits one.
	encoded, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("avoid: encode request: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("avoid: client is closed")
	}
	if _, err := c.stdin.Write(append(encoded, '\n')); err != nil {
		return nil, fmt.Errorf("avoid: write request: %w", err)
	}
	line, err := c.stdout.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("avoid: read response: %w", err)
	}
	var response Response
	if err := json.Unmarshal(line, &response); err != nil {
		return nil, fmt.Errorf("avoid: decode response: %w", err)
	}
	if response.Error != nil {
		return &response, response.Error
	}
	return &response, nil
}

// Close shuts the router down by closing its stdin and waiting for it to exit.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if err := c.stdin.Close(); err != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
		return fmt.Errorf("avoid: close stdin: %w", err)
	}
	if err := c.cmd.Wait(); err != nil {
		return fmt.Errorf("avoid: router exited: %w", err)
	}
	return nil
}
