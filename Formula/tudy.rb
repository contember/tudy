class Tudy < Formula
  desc "AI-powered local development proxy"
  homepage "https://github.com/contember/tudy"
  version "0.5.2"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/contember/tudy/releases/download/v#{version}/tudy-darwin-arm64.tar.gz"
      sha256 "ac6437c18d72bd4800534e8487ea7032da04337ed9971f21ee61bc3d2237897d"
    end
    on_intel do
      url "https://github.com/contember/tudy/releases/download/v#{version}/tudy-darwin-amd64.tar.gz"
      sha256 "e748ff955768969223520bdd773fc3132d3ce679219b8cd24fb438189d989651"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/contember/tudy/releases/download/v#{version}/tudy-linux-arm64.tar.gz"
      sha256 "1f6284c3d7f511a54ce130aa9456f49106762803d3af64aade8d884930814e86"
    end
    on_intel do
      url "https://github.com/contember/tudy/releases/download/v#{version}/tudy-linux-amd64.tar.gz"
      sha256 "7fb0155625b18d0d0a6639950f1332e5624aa4e1e7f26746133dc38b45999edb"
    end
  end

  def install
    bin.install "caddy" => "tudy-bin"
    bin.install "cli" => "tudy"
    (etc/"tudy").mkpath
    (etc/"tudy").install "Caddyfile" unless (etc/"tudy/Caddyfile").exist?
  end

  service do
    run [opt_bin/"tudy", "run", "--config", etc/"tudy/Caddyfile"]
    keep_alive true
    log_path var/"log/tudy.log"
    error_log_path var/"log/tudy.error.log"
    environment_variables XDG_DATA_HOME: HOMEBREW_PREFIX/"share", XDG_CONFIG_HOME: HOMEBREW_PREFIX/"etc"
  end

  def post_install
    (var/"lib/tudy").mkpath
    # Create Caddy data directory for PKI certificates
    (share/"caddy").mkpath
    # Create example env file if it doesn't exist
    env_file = etc/"tudy/env"
    unless env_file.exist?
      env_file.write <<~EOS
        # Add your API key here:
        # LLM_API_KEY=sk-your-key-here
        #
        # Optional settings:
        # LLM_API_URL=https://openrouter.ai/api/v1/chat/completions
        # MODEL=anthropic/claude-haiku-4.5
      EOS
    end
  end

  def caveats
    <<~EOS
      Run the interactive setup to configure API key, trust certificate, and start:
        tudy setup

      Or configure manually:
        echo "LLM_API_KEY=sk-your-key" >> #{etc}/tudy/env
        sudo brew services start tudy

      Logs: #{var}/log/tudy.log
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/tudy version")
  end
end
