#pragma once

#include "protocol.h"

namespace boxzavoid {

// Routes one request with a freshly constructed libavoid Router. Each request
// is independent: no state carries between requests on a long-lived process,
// so identical requests always produce identical responses.
Response RouteRequest(const Request& request);

}  // namespace boxzavoid
