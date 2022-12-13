#ifndef BOOST_JSON_HPP
#define BOOST_JSON_HPP

#include <common/json/json.hpp>

namespace json {

class BoostJson : public json::Json {
public:
	void hello_world();
};

} // namespace json

#endif // BOOST_JSON_HPP
