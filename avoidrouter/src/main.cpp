// boxz-avoid: an orthogonal edge router for boxz, driven over a pipe.
//
// The protocol is JSON Lines: one compact request object per line on stdin,
// one response object per line on stdout. EOF on stdin exits cleanly. This
// makes the one-shot case trivial to script:
//
//   echo '{"version":1,...}' | boxz-avoid
//
// while still letting boxz keep one warm process for repeated renders.
#include <iostream>
#include <string>

#include <nlohmann/json.hpp>

#include "protocol.h"
#include "route.h"

namespace {

void WriteLine(const nlohmann::json& value) {
  // dump() with no indent keeps every response on exactly one line.
  std::cout << value.dump() << '\n';
  std::cout.flush();
}

}  // namespace

int main(int argc, char** argv) {
  for (int i = 1; i < argc; ++i) {
    const std::string arg = argv[i];
    if (arg == "--version") {
      std::cout << "boxz-avoid protocol " << boxzavoid::kProtocolVersion << '\n';
      return 0;
    }
    if (arg == "-h" || arg == "--help") {
      std::cout << "usage: boxz-avoid < requests.jsonl > responses.jsonl\n\n"
                   "Reads one JSON request per line and writes one JSON response\n"
                   "per line. See README.md for the protocol.\n";
      return 0;
    }
    std::cerr << "boxz-avoid: unknown argument \"" << arg << "\"\n";
    return 2;
  }

  std::ios::sync_with_stdio(false);
  std::string line;
  while (std::getline(std::cin, line)) {
    // Tolerate blank lines so hand-written input files stay pleasant.
    if (line.find_first_not_of(" \t\r\n") == std::string::npos) {
      continue;
    }
    std::string id;
    try {
      const nlohmann::json value = nlohmann::json::parse(line);
      if (value.is_object()) {
        if (auto it = value.find("id"); it != value.end() && it->is_string()) {
          id = it->get<std::string>();
        }
      }
      const boxzavoid::Request request = boxzavoid::ParseRequest(value);
      WriteLine(boxzavoid::EncodeResponse(boxzavoid::RouteRequest(request)));
    } catch (const nlohmann::json::parse_error& e) {
      WriteLine(boxzavoid::EncodeError(id, "invalid_json", e.what()));
    } catch (const boxzavoid::RequestError& e) {
      WriteLine(boxzavoid::EncodeError(id, e.code, e.what()));
    } catch (const std::exception& e) {
      // A libavoid failure must not take down a long-lived process: report it
      // against this request and carry on with the next one.
      WriteLine(boxzavoid::EncodeError(id, "router_error", e.what()));
    }
  }
  return 0;
}
