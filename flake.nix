{
  description = "pgxcel — CEL to PostgreSQL WHERE transpiler";

  nixConfig = {
    extra-substituters = [ "https://ralch.cachix.org" ];
    extra-trusted-public-keys = [ "ralch.cachix.org-1:ti5RSqCJwd+VIv7dlxJEAVD98KIJDLxj1yJY+S80Q3A=" ];
  };

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    devcontainer-env.url = "github:devcontainer-env/devcontainer-env";
  };

  outputs =
    {
      nixpkgs,
      flake-utils,
      devcontainer-env,
      ...
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        devShells.default = pkgs.mkShell {
          name = "pgxcel";
          packages = [
            devcontainer-env.packages.${system}.default
            pkgs.go
          ];
          shellHook = ''
            eval "$(devcontainer-env export)"
          '';
        };
      }
    );
}
