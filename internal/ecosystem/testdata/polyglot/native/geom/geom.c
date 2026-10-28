#include "geom.h"

static int iabs(int v) { return v < 0 ? -v : v; }

int manhattan(struct point a, struct point b)
{
    return iabs(a.x - b.x) + iabs(a.y - b.y);
}
