#pragma once

#include <array>
#include <cstddef>
#include <optional>

// RingBuffer is a FIFO queue holding at most N values. push refuses a value
// when the buffer is full; pop returns the oldest value, or nothing when
// the buffer is empty.
template <typename T, std::size_t N>
class RingBuffer {
  public:
    bool push(const T &v) {
        if (full()) {
            return false;
        }
        data_[tail_] = v;
        tail_ = (tail_ + 1) % N;
        if (tail_ == head_) {
            full_ = true;
        }
        return true;
    }

    std::optional<T> pop() {
        if (empty()) {
            return std::nullopt;
        }
        T v = data_[head_];
        head_ = (head_ + 1) % N;
        full_ = false;
        return v;
    }

    bool empty() const { return head_ == tail_ && !full_; }
    bool full() const { return full_; }

    // size is the number of values held.
    std::size_t size() const { return tail_ - head_; }

    std::size_t capacity() const { return N; }

  private:
    std::array<T, N> data_{};
    std::size_t head_ = 0;
    std::size_t tail_ = 0;
    bool full_ = false;
};
