#ifndef BOOST_JSON_HPP
#define BOOST_JSON_HPP

#include <common/json/json.hpp>


namespace json
{

class BoostJson : public json::Json
{
   public:
    void hello_world();
};

}

#endif  // BOOST_JSON_HPP
