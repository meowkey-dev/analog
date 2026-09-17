{
  description = "Analog — a shared canvas for one human and their agents";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      supportedSystems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;
    in
    {
      packages = forAllSystems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          version = self.shortRev or self.dirtyShortRev or "dev";
          buildGoModule = pkgs.buildGoModule.override { go = pkgs.go_1_25; };
          web = pkgs.buildNpmPackage {
            pname = "analog-web";
            inherit version;
            src = self;
            npmDeps = pkgs.fetchNpmDeps {
              name = "analog-web-${version}-npm-deps";
              src = ./web;
              hash = "sha256-8QpIiqhJ62z20DgTl7Q93mxyLohSLXRjqjnlLy4fIEk=";
            };
            npmRoot = "web";
            buildPhase = ''
              runHook preBuild
              npm run build --prefix web
              runHook postBuild
            '';
            installPhase = ''
              runHook preInstall
              mkdir -p $out
              cp -R web/dist/. $out/
              runHook postInstall
            '';
          };
          analog = buildGoModule {
            pname = "analog";
            inherit version;
            src = self;

            vendorHash = "sha256-921E7nrOlWUoeNf94FVqkB39Fpbw+4rVkoN806UZmP0=";
            overrideModAttrs = _: {
              preBuild = null;
            };

            env.CGO_ENABLED = 0;
            subPackages = [
              "cmd/analog"
              "cmd/analog-server"
              "cmd/analog-mcp"
            ];
            ldflags = [
              "-s"
              "-w"
              "-X github.com/meowkey-dev/analog/internal/version.Version=${version}"
            ];

            preBuild = ''
              find internal/web/dist -mindepth 1 ! -name .gitkeep -delete
              cp -R ${web}/. internal/web/dist/
              chmod -R u+w internal/web/dist
              find internal/web/dist -name '*.map' -delete
            '';

            meta = {
              description = "Shared canvas for one human and their agents";
              homepage = "https://github.com/meowkey-dev/analog";
              license = pkgs.lib.licenses.asl20;
              mainProgram = "analog";
            };
          };
        in
        {
          default = analog;
          inherit analog;
        });

      devShells = forAllSystems (system:
        let pkgs = nixpkgs.legacyPackages.${system};
        in {
          default = pkgs.mkShell {
            packages = [ pkgs.go_1_25 pkgs.nodejs_22 ];
          };
        });

      nixosModules.default = import ./nix/module.nix { inherit self; };
    };
}
