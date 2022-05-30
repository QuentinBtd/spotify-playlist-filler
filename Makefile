PROJECT_VERSION="$(shell cat VERSION)"
PROJECT_NAME="spotify-playlist-filler"

# Defines the default target that `make` will to try to make,
# or in the case of a phony target, execute the specified commands
# This target is executed whenever we just type `make`
.DEFAULT_GOAL = help

# The @ makes sure that the command itself isn't echoed in the terminal
help: # Print help on Makefile
	@echo "Sample project version $(PROJECT_VERSION)"
	@echo ""
	@echo "Please use 'make <target>' where <target> is one of"
	@echo ""
	@grep '^[^.#]\+:\s\+.*#' Makefile | \
	sed "s/\(.\+\):\s*\(.*\) #\s*\(.*\)/`printf "\033[93m"`  \1`printf "\033[0m"`	\3 [\2]/" | \
	expand -35
	@echo ""
	@echo "Check the Makefile to know exactly what each target is doing."

exec: # Execute this program
	@go run src/main.go

build: # Build this program for all platforms
	@bash build.sh $(PROJECT_NAME)
