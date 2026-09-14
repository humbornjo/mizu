package app

import "example.com/mizucue/test/lib"

#TestModel: {
	name:     string
	age:      int & >=0 & <=150
	email:    string & =~"^[^@]+@[^@]+$"
	tags:     [...string]
	status:   lib.#Status
	metadata: [string]: string
	owner:    lib.#TestNamed
	address?: #TestAddress
}

#TestAddress: {
	street:  string
	city:    string
	zipCode: string & =~"^[0-9]{5}$"
}

#Config: {
	debug:  bool
	shared: lib.#Config
}

#ExtraModel: {
	name: string
}

#IncompleteModel: {
	value: string
}

#WrongTypeModel: {
	count: int
}
