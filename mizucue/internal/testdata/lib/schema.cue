package lib

#TestNamed: {
	name: string
}

#Status: "active" | "inactive" | "pending"

#Config: {
	endpoint: string
	retry:    int & >=0
}
