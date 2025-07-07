PREFIX=mizu.example

clean: gen
	rm -rf dist

gen:
	buf generate
	mv dist/$(PREFIX)/* ./

.PHONY: all clean
