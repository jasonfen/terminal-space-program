# Builds this checkout with nixpkgs' own terminal-space-program recipe:
# the same ldflags version stamp, test run and versionCheckHook that
# r-ryantm's update PRs go through. If this fails, the nixpkgs package
# stops updating, so CI runs it on every PR.
#
#   nix-build nix/nixpkgs-build.nix --argstr vendorHash sha256-...
#
# vendorHash changes whenever go.mod/go.sum do; pass lib.fakeHash (the
# default) once and the error prints the real one (CI does this).
{
  pkgs ? import <nixpkgs> { },
  # A release-shaped version, so length-sensitive output is exercised the
  # way a real nixpkgs build exercises it.
  version ? "0.99.99",
  vendorHash ? pkgs.lib.fakeHash,
}:
pkgs.terminal-space-program.overrideAttrs (_: {
  inherit version vendorHash;
  src = pkgs.lib.cleanSource ../.;
})
