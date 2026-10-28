#pragma once

namespace shop {

class Vec {
public:
    Vec(double x, double y);
    double dot(const Vec& o) const;

private:
    double x_, y_;
};

}  // namespace shop
