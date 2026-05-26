class Tudy < Formula
  desc "AI-powered local development proxy"
  homepage "https://github.com/contember/tudy"
  version "0.6.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/contember/tudy/releases/download/v#{version}/tudy-darwin-arm64.tar.gz"
      sha256 "70f8ffa80e2591db530b24969ff272d2f3265642d92bd6bf7526a520f670728b"
    end
    on_intel do
      url "https://github.com/contember/tudy/releases/download/v#{version}/tudy-darwin-amd64.tar.gz"
      sha256 "84c0cc638dff1be6985301693792425a694c672bac8055b1b645ae48807e3158"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/contember/tudy/releases/download/v#{version}/tudy-linux-arm64.tar.gz"
      sha256 "a3af47c685c541e55e917f51b21b0c51453d7d363fefee5db48989fdbbc8b400"
    end
    on_intel do
      url "https://github.com/contember/tudy/releases/download/v#{version}/tudy-linux-amd64.tar.gz"
      sha256 "efadd9f5aa6c8c6e8c1d80038245e88093f9ee1eced585dfe69bdd62a59c9d71"
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
