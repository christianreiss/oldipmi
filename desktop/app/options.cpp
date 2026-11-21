#include "app/options.h"
#include <algorithm>
#include <cstring>

bool selfTestRequested(int argc, char **argv) {
  return std::any_of(argv + 1, argv + argc, [](const char *a) { return !strcmp(a, "--self-test"); });
}

Options parseOptions(const QStringList &arguments, bool test) {
  Options o;
  o.test = test;
  o.preview = arguments.contains("--preview");
  o.probe = arguments.contains("--probe-ui");
  o.agentApi = !arguments.contains("--no-agent-api");
  const int shots = arguments.indexOf("--screenshots");
  // Only with --self-test: the tour deletes profiles, which must be test-mode copies.
  o.tour = test && shots > 0 && shots + 1 < arguments.size();
  if (!o.tour)
    return o;
  o.tourDir = arguments[shots + 1];
  if (int s = arguments.indexOf("--size"); s > 0 && s + 1 < arguments.size()) {
    auto wh = arguments[s + 1].split('x');
    if (wh.size() == 2)
      o.tourSize = {wh[0].toInt(), wh[1].toInt()};
    o.badSize = o.tourSize.isEmpty();
  }
  return o;
}
