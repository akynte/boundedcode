#include "vec.hpp"

namespace shop {

Vec::Vec(double x, double y) : x_(x), y_(y) {}

double Vec::dot(const Vec& o) const
{
    return x_ * o.x_ + y_ * o.y_;
}

}  // namespace shop
