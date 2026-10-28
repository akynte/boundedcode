#include "geom.h"

int main(void)
{
    struct point a = {0, 0}, b = {3, -4};
    return manhattan(a, b) == 7 ? 0 : 1;
}
