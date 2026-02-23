{
  description = "BuilderHub Build API - gRPC/HTTP gateway";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
        # migrate with postgres only - avoids snowflake CA cert panic in Nix
        migrate-postgres = pkgs.buildGoModule {
          pname = "migrate-postgres";
          version = "4.19.1";
          src = pkgs.fetchFromGitHub {
            owner = "golang-migrate";
            repo = "migrate";
            rev = "v4.19.1";
            sha256 = "sha256-Z8ufA2z5XeJ80Jfd6NSls/SurR8rMTO4zq88fQYGGpA=";
          };
          proxyVendor = true;
          vendorHash = "sha256-IaTNm119GO+1DkGYHFD8A8B/rWOVy0KAiXMhKj0zC/M=";
          subPackages = [ "cmd/migrate" ];
          tags = [ "postgres" "pgx" "pgx5" ];
        };
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go_1_25
            migrate-postgres
            buf
            gnumake
            docker
          ];

          shellHook = ''
            echo "BuilderHub Build API Dev Environment"
            echo ""
            echo "Run 'make build' to build the server"
            echo "Run 'make generate' to regenerate Go/gRPC from .proto"
            echo "Run 'make migrate-up' to run migrations (requires DATABASE_URL)"
            echo "Or trigger build-api-migrate in Tilt UI"
          '';
        };
      }
    );
}
