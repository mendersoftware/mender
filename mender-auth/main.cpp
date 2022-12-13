#include <iostream>
#include <memory>

using namespace std;

#include <common/json/json.hpp>

// This function only knows about the interface Json
void hello_world(std::shared_ptr<json::Json> j) {
	j->hello_world();
}

#include <common/json/impl/boost/boost_json.hpp>

int main() {
	// It is here that we make an object from a concrete type, BoostJson.
	shared_ptr<json::BoostJson> j = make_shared<json::BoostJson>();

	hello_world(j);
	cout << endl;

	return 0;
}
