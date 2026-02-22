{
  description = "BuilderHub Build API - gRPC + HTTP API for BuildKit builders";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachSystem [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ] (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            buf
            protoc-gen-go
            protoc-gen-go-grpc
            go-migrate
            tilt
            kubectl
            kind
            gnumake
          ];

          shellHook = ''
            echo "BuilderHub Build API Dev Environment"
            echo "Go: $(go version)"
            echo "Buf: $(buf --version 2>/dev/null || true)"
            echo ""
            echo "Run 'make generate' to generate proto code"
          '';
        };
      }
    );
}
