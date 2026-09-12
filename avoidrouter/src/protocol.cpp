#include "protocol.h"

#include <set>

namespace boxzavoid {
namespace {

using nlohmann::json;

[[noreturn]] void Invalid(const std::string& message) {
  throw RequestError("bad_request", message);
}

const json& Require(const json& object, const char* key, const std::string& where) {
  auto it = object.find(key);
  if (it == object.end()) {
    Invalid(where + ": missing \"" + key + "\"");
  }
  return *it;
}

double Number(const json& value, const std::string& where) {
  if (!value.is_number()) {
    Invalid(where + ": expected a number");
  }
  return value.get<double>();
}

std::string String(const json& value, const std::string& where) {
  if (!value.is_string()) {
    Invalid(where + ": expected a string");
  }
  return value.get<std::string>();
}

// Optional scalars leave the caller's default in place when absent, so adding
// a knob to the protocol never changes behaviour for older clients.
void ReadDouble(const json& object, const char* key, double* out) {
  auto it = object.find(key);
  if (it != object.end() && !it->is_null()) {
    *out = Number(*it, std::string("options.") + key);
  }
}

void ReadBool(const json& object, const char* key, bool* out) {
  auto it = object.find(key);
  if (it != object.end() && !it->is_null()) {
    if (!it->is_boolean()) {
      Invalid(std::string("options.") + key + ": expected a boolean");
    }
    *out = it->get<bool>();
  }
}

Point ReadPoint(const json& value, const std::string& where) {
  if (!value.is_object()) {
    Invalid(where + ": expected an object with x and y");
  }
  Point p;
  p.x = Number(Require(value, "x", where), where + ".x");
  p.y = Number(Require(value, "y", where), where + ".y");
  return p;
}

Rect ReadRect(const json& value, const std::string& where) {
  if (!value.is_object()) {
    Invalid(where + ": expected an object with x, y, w and h");
  }
  Rect r;
  r.x = Number(Require(value, "x", where), where + ".x");
  r.y = Number(Require(value, "y", where), where + ".y");
  r.w = Number(Require(value, "w", where), where + ".w");
  r.h = Number(Require(value, "h", where), where + ".h");
  if (r.w < 0 || r.h < 0) {
    Invalid(where + ": w and h must not be negative");
  }
  return r;
}

Endpoint ReadEndpoint(const json& value, const std::string& where) {
  if (!value.is_object()) {
    Invalid(where + ": expected an object");
  }
  Endpoint end;
  if (auto it = value.find("point"); it != value.end() && !it->is_null()) {
    end.point = ReadPoint(*it, where + ".point");
  }
  if (auto it = value.find("obstacle"); it != value.end() && !it->is_null()) {
    end.obstacle = String(*it, where + ".obstacle");
  }
  if (end.obstacle.empty() && !end.point.has_value()) {
    Invalid(where + ": needs either \"obstacle\" or \"point\"");
  }
  if (auto it = value.find("ports"); it != value.end() && !it->is_null()) {
    if (!it->is_array()) {
      Invalid(where + ".ports: expected an array of port ids");
    }
    for (const auto& port : *it) {
      end.ports.push_back(String(port, where + ".ports[]"));
    }
  }
  if (auto it = value.find("dirs"); it != value.end() && !it->is_null()) {
    if (!it->is_array()) {
      Invalid(where + ".dirs: expected an array of sides");
    }
    for (const auto& dir : *it) {
      end.dirs.push_back(ParseSide(String(dir, where + ".dirs[]")));
    }
  }
  return end;
}

void ReadOptions(const json& value, Options* out) {
  if (!value.is_object()) {
    Invalid("options: expected an object");
  }
  if (auto it = value.find("routingType"); it != value.end() && !it->is_null()) {
    const std::string type = String(*it, "options.routingType");
    if (type == "orthogonal") {
      out->orthogonal = true;
    } else if (type == "polyline") {
      out->orthogonal = false;
    } else {
      Invalid("options.routingType: expected \"orthogonal\" or \"polyline\"");
    }
  }
  ReadDouble(value, "shapeBufferDistance", &out->shape_buffer_distance);
  ReadDouble(value, "idealNudgingDistance", &out->ideal_nudging_distance);
  ReadDouble(value, "segmentPenalty", &out->segment_penalty);
  ReadDouble(value, "anglePenalty", &out->angle_penalty);
  ReadDouble(value, "crossingPenalty", &out->crossing_penalty);
  ReadDouble(value, "clusterCrossingPenalty", &out->cluster_crossing_penalty);
  ReadDouble(value, "fixedSharedPathPenalty", &out->fixed_shared_path_penalty);
  ReadDouble(value, "portDirectionPenalty", &out->port_direction_penalty);
  ReadDouble(value, "reverseDirectionPenalty", &out->reverse_direction_penalty);
  ReadBool(value, "nudgeOrthogonalSegmentsConnectedToShapes",
           &out->nudge_orthogonal_segments_connected_to_shapes);
  ReadBool(value, "nudgeSharedPathsWithCommonEndPoint",
           &out->nudge_shared_paths_with_common_end_point);
  ReadBool(value, "nudgeOrthogonalTouchingColinearSegments",
           &out->nudge_orthogonal_touching_colinear_segments);
  ReadBool(value, "performUnifyingNudgingPreprocessingStep",
           &out->perform_unifying_nudging_preprocessing_step);
  ReadBool(value, "penaliseOrthogonalSharedPathsAtConnEnds",
           &out->penalise_orthogonal_shared_paths_at_conn_ends);
  ReadBool(value, "improveHyperedgeRoutesMovingJunctions",
           &out->improve_hyperedge_routes_moving_junctions);
}

json EncodePoint(const Point& p) {
  return json{{"x", p.x}, {"y", p.y}};
}

json EncodeEnd(const ResolvedEnd& end) {
  json value{{"point", EncodePoint(end.point)}};
  if (!end.obstacle.empty()) {
    value["obstacle"] = end.obstacle;
  }
  if (end.port.has_value()) {
    value["port"] = *end.port;
  }
  if (end.side.has_value()) {
    value["side"] = SideName(*end.side);
  }
  return value;
}

}  // namespace

std::string SideName(Side side) {
  switch (side) {
    case Side::North: return "north";
    case Side::East:  return "east";
    case Side::South: return "south";
    case Side::West:  return "west";
  }
  return "north";
}

Side ParseSide(const std::string& name) {
  if (name == "north" || name == "n") return Side::North;
  if (name == "east"  || name == "e") return Side::East;
  if (name == "south" || name == "s") return Side::South;
  if (name == "west"  || name == "w") return Side::West;
  Invalid("side: expected north, east, south or west, got \"" + name + "\"");
}

Request ParseRequest(const json& value) {
  if (!value.is_object()) {
    Invalid("request: expected a JSON object");
  }
  Request request;
  if (auto it = value.find("version"); it != value.end() && !it->is_null()) {
    request.version = static_cast<int>(Number(*it, "version"));
    if (request.version != kProtocolVersion) {
      throw RequestError("unsupported_version",
                         "request version " + std::to_string(request.version) +
                             " is not supported (this build speaks version " +
                             std::to_string(kProtocolVersion) + ")");
    }
  }
  if (auto it = value.find("id"); it != value.end() && !it->is_null()) {
    request.id = String(*it, "id");
  }
  if (auto it = value.find("options"); it != value.end() && !it->is_null()) {
    ReadOptions(*it, &request.options);
  }

  // Obstacle and port ids must be unique: they are the response's only handle
  // on what the router chose, and duplicates would make routes ambiguous.
  std::set<std::string> obstacle_ids;
  if (auto it = value.find("obstacles"); it != value.end() && !it->is_null()) {
    if (!it->is_array()) {
      Invalid("obstacles: expected an array");
    }
    for (const auto& item : *it) {
      const std::string where = "obstacles[" + std::to_string(request.obstacles.size()) + "]";
      if (!item.is_object()) {
        Invalid(where + ": expected an object");
      }
      Obstacle obstacle;
      obstacle.id = String(Require(item, "id", where), where + ".id");
      if (!obstacle_ids.insert(obstacle.id).second) {
        Invalid(where + ": duplicate obstacle id \"" + obstacle.id + "\"");
      }
      obstacle.rect = ReadRect(Require(item, "rect", where), where + ".rect");
      if (auto ports = item.find("ports"); ports != item.end() && !ports->is_null()) {
        if (!ports->is_array()) {
          Invalid(where + ".ports: expected an array");
        }
        std::set<std::string> port_ids;
        for (const auto& entry : *ports) {
          const std::string port_where =
              where + ".ports[" + std::to_string(obstacle.ports.size()) + "]";
          if (!entry.is_object()) {
            Invalid(port_where + ": expected an object");
          }
          Port port;
          port.id = String(Require(entry, "id", port_where), port_where + ".id");
          if (!port_ids.insert(port.id).second) {
            Invalid(port_where + ": duplicate port id \"" + port.id + "\"");
          }
          port.side = ParseSide(String(Require(entry, "side", port_where), port_where + ".side"));
          if (auto pos = entry.find("pos"); pos != entry.end() && !pos->is_null()) {
            port.pos = Number(*pos, port_where + ".pos");
            if (port.pos < 0 || port.pos > 1) {
              Invalid(port_where + ".pos: must be between 0 and 1");
            }
          }
          obstacle.ports.push_back(std::move(port));
        }
      }
      ReadBool(item, "exclusivePorts", &obstacle.exclusive_ports);
      request.obstacles.push_back(std::move(obstacle));
    }
  }

  if (auto it = value.find("clusters"); it != value.end() && !it->is_null()) {
    if (!it->is_array()) {
      Invalid("clusters: expected an array");
    }
    for (const auto& item : *it) {
      const std::string where = "clusters[" + std::to_string(request.clusters.size()) + "]";
      Cluster cluster;
      cluster.id = String(Require(item, "id", where), where + ".id");
      cluster.rect = ReadRect(Require(item, "rect", where), where + ".rect");
      request.clusters.push_back(std::move(cluster));
    }
  }

  std::set<std::string> edge_ids;
  if (auto it = value.find("edges"); it != value.end() && !it->is_null()) {
    if (!it->is_array()) {
      Invalid("edges: expected an array");
    }
    for (const auto& item : *it) {
      const std::string where = "edges[" + std::to_string(request.edges.size()) + "]";
      if (!item.is_object()) {
        Invalid(where + ": expected an object");
      }
      Edge edge;
      edge.id = String(Require(item, "id", where), where + ".id");
      if (!edge_ids.insert(edge.id).second) {
        Invalid(where + ": duplicate edge id \"" + edge.id + "\"");
      }
      edge.from = ReadEndpoint(Require(item, "from", where), where + ".from");
      edge.to = ReadEndpoint(Require(item, "to", where), where + ".to");
      if (auto points = item.find("checkpoints"); points != item.end() && !points->is_null()) {
        if (!points->is_array()) {
          Invalid(where + ".checkpoints: expected an array");
        }
        for (const auto& entry : *points) {
          edge.checkpoints.push_back(ReadPoint(entry, where + ".checkpoints[]"));
        }
      }
      request.edges.push_back(std::move(edge));
    }
  }
  return request;
}

nlohmann::json EncodeResponse(const Response& response) {
  json routes = json::array();
  for (const auto& route : response.routes) {
    json points = json::array();
    for (const auto& p : route.points) {
      points.push_back(EncodePoint(p));
    }
    routes.push_back(json{{"id", route.id},
                          {"points", std::move(points)},
                          {"from", EncodeEnd(route.from)},
                          {"to", EncodeEnd(route.to)}});
  }
  json value{{"version", response.version},
             {"routes", std::move(routes)},
             {"stats", json{{"elapsedMs", response.stats.elapsed_ms},
                            {"obstacles", response.stats.obstacles},
                            {"edges", response.stats.edges}}}};
  if (!response.id.empty()) {
    value["id"] = response.id;
  }
  if (!response.warnings.empty()) {
    value["warnings"] = response.warnings;
  }
  return value;
}

nlohmann::json EncodeError(const std::string& id, const std::string& code,
                           const std::string& message) {
  json value{{"version", kProtocolVersion},
             {"error", json{{"code", code}, {"message", message}}}};
  if (!id.empty()) {
    value["id"] = id;
  }
  return value;
}

}  // namespace boxzavoid
