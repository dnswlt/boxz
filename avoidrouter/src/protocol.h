// Wire types for the boxz-avoid JSON protocol.
//
// The protocol is deliberately policy-free: it describes obstacles, candidate
// ports, and edges, and it returns polylines. Every boxz-specific decision
// (which containers become obstacles, how many ports a node offers, whether a
// side is constrained) is made in Go and arrives here already resolved.
#pragma once

#include <optional>
#include <string>
#include <vector>

#include <nlohmann/json.hpp>

namespace boxzavoid {

// Protocol version. Bumped only for incompatible changes; unknown optional
// fields are ignored so additive changes need no bump.
inline constexpr int kProtocolVersion = 1;

// Coordinates are SVG-style: x grows east, y grows south. This matches
// libavoid, whose ConnDirDown is the +y direction.
struct Point {
  double x = 0;
  double y = 0;
};

struct Rect {
  double x = 0;
  double y = 0;
  double w = 0;
  double h = 0;
};

enum class Side { North, East, South, West };

// A candidate attachment point on an obstacle's perimeter. `pos` is the
// fraction along the side, measured west-to-east on north/south and
// north-to-south on east/west.
struct Port {
  std::string id;
  Side side = Side::North;
  double pos = 0.5;
};

struct Obstacle {
  std::string id;
  Rect rect;
  std::vector<Port> ports;
  // When true (the default) libavoid gives each port to at most one edge.
  bool exclusive_ports = true;
};

// Clusters are penalised-crossing regions rather than hard obstacles. See
// README.md: libavoid ignores them under orthogonal routing, so they are
// accepted but currently inert.
struct Cluster {
  std::string id;
  Rect rect;
};

// An edge endpoint is one of three forms, checked in this order:
//   - `point` set          -> a fixed coordinate, with optional direction set
//   - `ports` non-empty    -> one of that named subset of the obstacle's ports
//   - otherwise            -> any of the obstacle's ports, or its centre when
//                             the obstacle declares none
struct Endpoint {
  std::string obstacle;
  std::vector<std::string> ports;
  std::optional<Point> point;
  std::vector<Side> dirs;
};

struct Edge {
  std::string id;
  Endpoint from;
  Endpoint to;
  std::vector<Point> checkpoints;
};

// Routing knobs passed straight through to libavoid. Defaults mirror
// libavoid's own, except where boxz wants something different.
struct Options {
  bool orthogonal = true;
  double shape_buffer_distance = 8;
  double ideal_nudging_distance = 8;
  double segment_penalty = 50;
  double angle_penalty = 0;
  double crossing_penalty = 0;
  double cluster_crossing_penalty = 4000;
  double fixed_shared_path_penalty = 0;
  double port_direction_penalty = 100;
  double reverse_direction_penalty = 0;
  bool nudge_orthogonal_segments_connected_to_shapes = true;
  bool nudge_shared_paths_with_common_end_point = true;
  bool nudge_orthogonal_touching_colinear_segments = false;
  bool perform_unifying_nudging_preprocessing_step = true;
  bool penalise_orthogonal_shared_paths_at_conn_ends = false;
  bool improve_hyperedge_routes_moving_junctions = false;
};

struct Request {
  int version = kProtocolVersion;
  std::string id;
  Options options;
  std::vector<Obstacle> obstacles;
  std::vector<Cluster> clusters;
  std::vector<Edge> edges;
};

// Where an edge actually attached, so boxz can render ports and diagnose
// choices it did not make itself.
struct ResolvedEnd {
  std::string obstacle;
  std::optional<std::string> port;
  std::optional<Side> side;
  Point point;
};

struct Route {
  std::string id;
  std::vector<Point> points;
  ResolvedEnd from;
  ResolvedEnd to;
};

struct Stats {
  double elapsed_ms = 0;
  int obstacles = 0;
  int edges = 0;
};

struct Response {
  int version = kProtocolVersion;
  std::string id;
  std::vector<Route> routes;
  std::vector<std::string> warnings;
  Stats stats;
};

// Thrown for malformed or semantically invalid requests. main() turns it into
// an error response rather than a crash, so one bad request does not kill a
// long-lived router process.
struct RequestError : std::runtime_error {
  RequestError(std::string code, const std::string& message)
      : std::runtime_error(message), code(std::move(code)) {}
  std::string code;
};

std::string SideName(Side side);
Side ParseSide(const std::string& name);

Request ParseRequest(const nlohmann::json& value);
nlohmann::json EncodeResponse(const Response& response);
nlohmann::json EncodeError(const std::string& id, const std::string& code,
                           const std::string& message);

}  // namespace boxzavoid
