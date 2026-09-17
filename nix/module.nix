{ self }:
{ config, lib, pkgs, ... }:
let
  cfg = config.services.analog;
in
{
  options.services.analog = {
    enable = lib.mkEnableOption "the Analog shared-canvas server";

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      defaultText = lib.literalExpression "inputs.analog.packages.\${pkgs.stdenv.hostPlatform.system}.default";
      description = "Analog package to run.";
    };

    port = lib.mkOption {
      type = lib.types.port;
      default = 8787;
      description = "Loopback port on which analog-server listens.";
    };

    dataDir = lib.mkOption {
      type = lib.types.path;
      default = "/var/lib/analog";
      description = "Directory containing the database, media, and authentication state.";
    };
  };

  config = lib.mkIf cfg.enable {
    users.groups.analog = { };
    users.users.analog = {
      isSystemUser = true;
      group = "analog";
      home = cfg.dataDir;
    };

    systemd.tmpfiles.rules = [ "d ${cfg.dataDir} 0750 analog analog -" ];
    systemd.services.analog = {
      description = "Analog — a shared canvas for one human and their agents";
      wantedBy = [ "multi-user.target" ];
      wants = [ "network-online.target" ];
      after = [ "network-online.target" ];
      environment.ANALOG_DATA_DIR = cfg.dataDir;
      serviceConfig = {
        ExecStart = "${lib.getExe' cfg.package "analog-server"} --host 127.0.0.1 --port ${toString cfg.port}";
        User = "analog";
        Group = "analog";
        Restart = "on-failure";
        RestartSec = 3;
        NoNewPrivileges = true;
        PrivateTmp = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        ReadWritePaths = [ cfg.dataDir ];
      };
    };
  };
}
