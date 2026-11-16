{
  description = "Terminal-native orbital-mechanics rocket simulator";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      forAllSystems = nixpkgs.lib.genAttrs [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
    in
    {
      packages = forAllSystems (system: {
        # nixpkgs' own recipe built from this checkout, as nix/nixpkgs-build.nix does.
        default = nixpkgs.legacyPackages.${system}.terminal-space-program.overrideAttrs (old: {
          version = "dev";
          src = self;
          # Changes whenever go.mod/go.sum do: CI's flake build fails and prints the new one.
          vendorHash = "sha256-DtzGzLe5HMasMZdfO3h1VTBKGFuANyQExY/2mireTEE=";
          ldflags = old.ldflags ++ [
            "-X github.com/jasonfen/terminal-space-program/internal/version.Commit=${self.shortRev or "dirty"}"
          ];
          # CI runs the test suite; an install should not.
          doCheck = false;
        });
      });
    };
}
