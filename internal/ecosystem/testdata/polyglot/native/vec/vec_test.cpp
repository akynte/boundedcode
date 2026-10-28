#include "vec.hpp"

int main()
{
    shop::Vec a(1, 2), b(3, 4);
    return a.dot(b) == 11 ? 0 : 1;
}
