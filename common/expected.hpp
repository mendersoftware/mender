// Copyright 2023 Northern.tech AS
//
//    Licensed under the Apache License, Version 2.0 (the "License");
//    you may not use this file except in compliance with the License.
//    You may obtain a copy of the License at
//
//        http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS,
//    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//    See the License for the specific language governing permissions and
//    limitations under the License.

#ifndef MENDER_COMMON_EXPECTED_HPP
#define MENDER_COMMON_EXPECTED_HPP

#include <common/error.hpp>

#include <cassert>
#include <cstdint>
#include <string>
#include <vector>

namespace mender {
namespace common {
namespace expected {

template <typename ExpectedType, typename ErrorType>
class Expected {
public:
	Expected(const ExpectedType &ex);
	Expected(const ErrorType &err);
	Expected(const Expected &e);
	Expected(Expected &&e);

	Expected(ExpectedType &&ex);
	Expected &operator=(Expected &&ex);

	~Expected();

	bool has_value() const {
		return has_val_;
	};
	ExpectedType &value();
	const ExpectedType &value() const;
	ErrorType error() const;

	Expected &operator=(const Expected &);
	operator bool() const {
		return has_val_;
	};

private:
	bool has_val_;
	union {
		ExpectedType ex_;
		ErrorType err_;
	};
};

using ExpectedString = Expected<std::string, error::Error>;
using ExpectedBytes = expected::Expected<std::vector<uint8_t>, error::Error>;
using ExpectedInt = Expected<int, error::Error>;
using ExpectedLong = Expected<long, error::Error>;
using ExpectedLongLong = Expected<long long, error::Error>;
using ExpectedBool = Expected<bool, error::Error>;
using ExpectedSize = Expected<size_t, error::Error>;

template <typename ExpectedType, typename ErrorType>
Expected<ExpectedType, ErrorType>::Expected(const Expected &e) {
	if (e.has_val_) {
		this->has_val_ = true;
		new (&this->ex_) ExpectedType(e.value());
	} else {
		this->has_val_ = false;
		new (&this->err_) ErrorType(e.error());
	}
}


template <typename ExpectedType, typename ErrorType>
Expected<ExpectedType, ErrorType>::Expected(Expected &&e) {
	if (e.has_val_) {
		this->has_val_ = true;
		new (&this->ex_) ExpectedType(std::move(e.value()));
	} else {
		this->has_val_ = false;
		new (&this->err_) ErrorType(e.error());
	}
}

template <typename ExpectedType, typename ErrorType>
Expected<ExpectedType, ErrorType>::Expected(const ExpectedType &ex) :
	has_val_(true),
	ex_(ex) {};

template <typename ExpectedType, typename ErrorType>
Expected<ExpectedType, ErrorType>::Expected(ExpectedType &&ex) :
	has_val_(true),
	ex_(std::move(ex)) {};

template <typename ExpectedType, typename ErrorType>
Expected<ExpectedType, ErrorType>::Expected(const ErrorType &err) :
	has_val_(false),
	err_(err) {};

template <typename ExpectedType, typename ErrorType>
Expected<ExpectedType, ErrorType>::~Expected() {
	if (this->has_val_) {
		this->ex_.~ExpectedType();
	} else {
		this->err_.~ErrorType();
	}
};

template <typename ExpectedType, typename ErrorType>
ExpectedType &Expected<ExpectedType, ErrorType>::value() {
	assert(this->has_val_);
	return this->ex_;
};

template <typename ExpectedType, typename ErrorType>
const ExpectedType &Expected<ExpectedType, ErrorType>::value() const {
	assert(this->has_val_);
	return this->ex_;
};

template <typename ExpectedType, typename ErrorType>
ErrorType Expected<ExpectedType, ErrorType>::error() const {
	assert(!this->has_val_);
	return this->err_;
};

template <typename ExpectedType, typename ErrorType>
Expected<ExpectedType, ErrorType> &Expected<ExpectedType, ErrorType>::operator=(const Expected &e) {
	if (this->has_val_) {
		this->ex_.~ExpectedType();
	} else {
		this->err_.~ErrorType();
	}

	this->has_val_ = e.has_val_;
	if (e.has_val_) {
		this->has_val_ = true;
		new (&this->ex_) ExpectedType(e.value());
	} else {
		this->has_val_ = false;
		new (&this->err_) ErrorType(e.error());
	}
	return *this;
}

template <typename ExpectedType, typename ErrorType>
Expected<ExpectedType, ErrorType> &Expected<ExpectedType, ErrorType>::operator=(Expected &&e) {
	if (this->has_val_) {
		this->ex_.~ExpectedType();
	} else {
		this->err_.~ErrorType();
	}

	this->has_val_ = e.has_val_;
	if (e.has_val_) {
		this->has_val_ = true;
		new (&this->ex_) ExpectedType(std::move(e.value()));
	} else {
		this->has_val_ = false;
		new (&this->err_) ErrorType(std::move(e.error()));
	}
	return *this;
}

} // namespace expected
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_EXPECTED_HPP
