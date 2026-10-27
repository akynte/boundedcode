# Plain javac, no build tool: `make test` compiles and runs every *Test class.
# The JDK is $JAVA_HOME's when it is set, otherwise the one on PATH.
JAVA_BIN := $(if $(JAVA_HOME),$(JAVA_HOME)/bin/,)
SOURCES := $(shell find src -name '*.java')

build:
	$(JAVA_BIN)javac -d out $(SOURCES)

test: build
	@set -e; for t in $$(cd src && find . -name '*Test.java' | sed 's|^\./||; s|\.java$$||; s|/|.|g'); do $(JAVA_BIN)java -cp out $$t; done

clean:
	rm -rf out

.PHONY: build test clean
