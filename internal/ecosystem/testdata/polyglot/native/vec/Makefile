CXX ?= g++
CXXFLAGS ?= -std=c++17 -Wall -O0

all: vec_test

vec_test: vec.cpp vec_test.cpp vec.hpp
	$(CXX) $(CXXFLAGS) -o $@ vec.cpp vec_test.cpp

test: vec_test
	./vec_test

clean:
	rm -f vec_test
