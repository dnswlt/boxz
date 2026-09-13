#include "route.h"

#include <chrono>
#include <cmath>
#include <map>
#include <set>
#include <sstream>
#include <unordered_map>

#include "libavoid/libavoid.h"

namespace boxzavoid {
namespace {

// libavoid's Up/Down are y-axis directions, not screen directions: ConnDirDown
// is +y. Because boxz already works in SVG coordinates (y grows south), the
// mapping is the identity and no axis flip is needed anywhere in this file.
Avoid::ConnDirFlag DirFor(Side side) {
  switch (side) {
    case Side::North: return Avoid::ConnDirUp;
    case Side::East:  return Avoid::ConnDirRight;
    case Side::South: return Avoid::ConnDirDown;
    case Side::West:  return Avoid::ConnDirLeft;
  }
  return Avoid::ConnDirAll;
}

Avoid::ConnDirFlags DirsFor(const std::vector<Side>& sides) {
  if (sides.empty()) {
    return Avoid::ConnDirAll;
  }
  unsigned int flags = 0;
  for (Side side : sides) {
    flags |= DirFor(side);
  }
  return static_cast<Avoid::ConnDirFlags>(flags);
}

// A port's position as proportional offsets within the shape's bounding box,
// which is how ShapeConnectionPin wants it.
void PortOffsets(const Port& port, double* x, double* y) {
  switch (port.side) {
    case Side::North: *x = port.pos; *y = 0.0;      break;
    case Side::South: *x = port.pos; *y = 1.0;      break;
    case Side::West:  *x = 0.0;      *y = port.pos; break;
    case Side::East:  *x = 1.0;      *y = port.pos; break;
  }
}

Point PortPoint(const Rect& rect, const Port& port) {
  double fx = 0, fy = 0;
  PortOffsets(port, &fx, &fy);
  return Point{rect.x + rect.w * fx, rect.y + rect.h * fy};
}

// The key identifying one distinct set of eligible ports on an obstacle. An
// empty key means "any declared port".
std::string SelectionKey(const std::vector<std::string>& ports) {
  std::ostringstream key;
  for (const auto& port : ports) {
    key << port << '\x1f';
  }
  return key.str();
}

struct ObstacleState {
  const Obstacle* spec = nullptr;
  Avoid::ShapeRef* shape = nullptr;
  // Pin class per distinct eligible-port set, allocated on first use.
  std::map<std::string, unsigned int> pin_classes;
  // Port id -> resolved coordinate, for reporting what libavoid chose.
  std::vector<std::pair<std::string, Point>> port_points;
  unsigned int next_class = 1;
  bool centre_pin = false;
};

double DistanceSquared(const Point& a, const Point& b) {
  const double dx = a.x - b.x;
  const double dy = a.y - b.y;
  return dx * dx + dy * dy;
}

}  // namespace

Response RouteRequest(const Request& request) {
  const auto started = std::chrono::steady_clock::now();

  Avoid::Router router(request.options.orthogonal ? Avoid::OrthogonalRouting
                                                  : Avoid::PolyLineRouting);
  const Options& opt = request.options;
  router.setRoutingParameter(Avoid::shapeBufferDistance, opt.shape_buffer_distance);
  router.setRoutingParameter(Avoid::idealNudgingDistance, opt.ideal_nudging_distance);
  router.setRoutingParameter(Avoid::segmentPenalty, opt.segment_penalty);
  router.setRoutingParameter(Avoid::anglePenalty, opt.angle_penalty);
  router.setRoutingParameter(Avoid::crossingPenalty, opt.crossing_penalty);
  router.setRoutingParameter(Avoid::clusterCrossingPenalty, opt.cluster_crossing_penalty);
  router.setRoutingParameter(Avoid::fixedSharedPathPenalty, opt.fixed_shared_path_penalty);
  router.setRoutingParameter(Avoid::portDirectionPenalty, opt.port_direction_penalty);
  router.setRoutingParameter(Avoid::reverseDirectionPenalty, opt.reverse_direction_penalty);
  router.setRoutingOption(Avoid::nudgeOrthogonalSegmentsConnectedToShapes,
                          opt.nudge_orthogonal_segments_connected_to_shapes);
  router.setRoutingOption(Avoid::nudgeSharedPathsWithCommonEndPoint,
                          opt.nudge_shared_paths_with_common_end_point);
  router.setRoutingOption(Avoid::nudgeOrthogonalTouchingColinearSegments,
                          opt.nudge_orthogonal_touching_colinear_segments);
  router.setRoutingOption(Avoid::performUnifyingNudgingPreprocessingStep,
                          opt.perform_unifying_nudging_preprocessing_step);
  router.setRoutingOption(Avoid::penaliseOrthogonalSharedPathsAtConnEnds,
                          opt.penalise_orthogonal_shared_paths_at_conn_ends);
  router.setRoutingOption(Avoid::improveHyperedgeRoutesMovingJunctions,
                          opt.improve_hyperedge_routes_moving_junctions);

  Response response;
  response.id = request.id;

  // Shapes are created in request order and given sequential ids, so libavoid
  // sees the same input in the same order for equal requests.
  unsigned int next_id = 1;
  std::unordered_map<std::string, ObstacleState> obstacles;
  for (const Obstacle& spec : request.obstacles) {
    Avoid::Rectangle rectangle(
        Avoid::Point(spec.rect.x, spec.rect.y),
        Avoid::Point(spec.rect.x + spec.rect.w, spec.rect.y + spec.rect.h));
    ObstacleState state;
    state.spec = &spec;
    state.shape = new Avoid::ShapeRef(&router, rectangle, next_id++);
    for (const Port& port : spec.ports) {
      state.port_points.emplace_back(port.id, PortPoint(spec.rect, port));
    }
    obstacles.emplace(spec.id, std::move(state));
  }

  for (const Cluster& cluster : request.clusters) {
    Avoid::Polygon poly(4);
    poly.ps[0] = Avoid::Point(cluster.rect.x, cluster.rect.y);
    poly.ps[1] = Avoid::Point(cluster.rect.x + cluster.rect.w, cluster.rect.y);
    poly.ps[2] = Avoid::Point(cluster.rect.x + cluster.rect.w,
                              cluster.rect.y + cluster.rect.h);
    poly.ps[3] = Avoid::Point(cluster.rect.x, cluster.rect.y + cluster.rect.h);
    new Avoid::ClusterRef(&router, poly, next_id++);
  }
  if (!request.clusters.empty() && request.options.orthogonal) {
    response.warnings.push_back(
        "clusters are ignored under orthogonal routing: libavoid's cluster "
        "crossing penalty only applies to polyline routes");
  }

  // Resolves an endpoint to a ConnEnd, creating pins on demand. Ports are only
  // materialised for the eligible sets that edges actually ask for, so an
  // obstacle nothing connects to costs no pins.
  auto resolve = [&](const Endpoint& end, const std::string& where) -> Avoid::ConnEnd {
    if (end.point.has_value()) {
      return Avoid::ConnEnd(Avoid::Point(end.point->x, end.point->y), DirsFor(end.dirs));
    }
    auto it = obstacles.find(end.obstacle);
    if (it == obstacles.end()) {
      throw RequestError("unknown_obstacle",
                         where + ": no obstacle with id \"" + end.obstacle + "\"");
    }
    ObstacleState& state = it->second;
    if (state.spec->ports.empty()) {
      if (!end.ports.empty()) {
        throw RequestError("unknown_port",
                           where + ": obstacle \"" + end.obstacle + "\" declares no ports");
      }
      // No candidate ports: attach to the shape centre and let libavoid leave
      // by whichever side the route prefers. The centre pin is not implicit --
      // without it libavoid warns and drops the endpoint's visibility.
      if (!state.centre_pin) {
        new Avoid::ShapeConnectionPin(state.shape, Avoid::CONNECTIONPIN_CENTRE,
                                      Avoid::ATTACH_POS_CENTRE, Avoid::ATTACH_POS_CENTRE,
                                      /*proportional=*/true, /*insideOffset=*/0.0,
                                      Avoid::ConnDirAll);
        state.centre_pin = true;
      }
      return Avoid::ConnEnd(state.shape, Avoid::CONNECTIONPIN_CENTRE);
    }

    std::set<std::string> wanted(end.ports.begin(), end.ports.end());
    for (const auto& id : wanted) {
      bool known = false;
      for (const auto& port : state.spec->ports) {
        known = known || port.id == id;
      }
      if (!known) {
        throw RequestError("unknown_port", where + ": obstacle \"" + end.obstacle +
                                               "\" has no port \"" + id + "\"");
      }
    }

    const std::string key = SelectionKey(end.ports);
    auto existing = state.pin_classes.find(key);
    if (existing != state.pin_classes.end()) {
      return Avoid::ConnEnd(state.shape, existing->second);
    }
    const unsigned int pin_class = state.next_class++;
    state.pin_classes.emplace(key, pin_class);
    for (const Port& port : state.spec->ports) {
      if (!wanted.empty() && wanted.find(port.id) == wanted.end()) {
        continue;
      }
      double fx = 0, fy = 0;
      PortOffsets(port, &fx, &fy);
      auto* pin = new Avoid::ShapeConnectionPin(state.shape, pin_class, fx, fy,
                                                /*proportional=*/true,
                                                /*insideOffset=*/0.0,
                                                DirFor(port.side));
      pin->setExclusive(state.spec->exclusive_ports);
    }
    return Avoid::ConnEnd(state.shape, pin_class);
  };

  std::vector<Avoid::ConnRef*> conns;
  conns.reserve(request.edges.size());
  for (const Edge& edge : request.edges) {
    const std::string where = "edge \"" + edge.id + "\"";
    Avoid::ConnEnd from = resolve(edge.from, where + ".from");
    Avoid::ConnEnd to = resolve(edge.to, where + ".to");
    auto* conn = new Avoid::ConnRef(&router, from, to, next_id++);
    conn->setRoutingType(request.options.orthogonal ? Avoid::ConnType_Orthogonal
                                                    : Avoid::ConnType_PolyLine);
    if (!edge.checkpoints.empty()) {
      std::vector<Avoid::Checkpoint> checkpoints;
      checkpoints.reserve(edge.checkpoints.size());
      for (const Point& p : edge.checkpoints) {
        checkpoints.emplace_back(Avoid::Point(p.x, p.y));
      }
      conn->setRoutingCheckpoints(checkpoints);
    }
    conns.push_back(conn);
  }

  router.processTransaction();

  // Identify the chosen port by the pin libavoid attached to. Nudging can move
  // the display endpoint along the border, but ConnEnd::position() reports the
  // active pin, which stays where it was declared.
  auto describe = [&](const Endpoint& end, const Avoid::ConnEnd& attached,
                      const Point& terminal) {
    ResolvedEnd resolved;
    resolved.point = terminal;
    if (end.point.has_value()) {
      return resolved;
    }
    resolved.obstacle = end.obstacle;
    auto it = obstacles.find(end.obstacle);
    if (it == obstacles.end()) {
      return resolved;
    }
    const ObstacleState& state = it->second;
    const Avoid::Point pin = attached.position();
    double best = 1e-6;
    for (size_t index = 0; index < state.port_points.size(); ++index) {
      const double distance = DistanceSquared(state.port_points[index].second, Point{pin.x, pin.y});
      if (distance <= best) {
        best = distance;
        resolved.port = state.port_points[index].first;
        resolved.side = state.spec->ports[index].side;
      }
    }
    return resolved;
  };

  for (size_t index = 0; index < request.edges.size(); ++index) {
    const Edge& edge = request.edges[index];
    const Avoid::PolyLine& line = conns[index]->displayRoute();
    Route route;
    route.id = edge.id;
    route.points.reserve(line.size());
    for (size_t i = 0; i < line.size(); ++i) {
      route.points.push_back(Point{line.ps[i].x, line.ps[i].y});
    }
    if (route.points.empty()) {
      response.warnings.push_back("edge \"" + edge.id + "\": libavoid returned no route");
    } else {
      const std::pair<Avoid::ConnEnd, Avoid::ConnEnd> ends = conns[index]->endpointConnEnds();
      route.from = describe(edge.from, ends.first, route.points.front());
      route.to = describe(edge.to, ends.second, route.points.back());
    }
    response.routes.push_back(std::move(route));
  }

  response.stats.obstacles = static_cast<int>(request.obstacles.size());
  response.stats.edges = static_cast<int>(request.edges.size());
  response.stats.elapsed_ms =
      std::chrono::duration<double, std::milli>(std::chrono::steady_clock::now() - started)
          .count();
  return response;
}

}  // namespace boxzavoid
