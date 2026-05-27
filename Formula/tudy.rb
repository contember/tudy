class Tudy < Formula
  desc "AI-powered local development proxy"
  homepage "https://github.com/contember/tudy"
  version "0.7.2"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/contember/tudy/releases/download/v#{version}/tudy-darwin-arm64.tar.gz"
      sha256 "a37ba98957976ebd89886195999592b523c4c1dcc017f23732d8d411956ed5b9"
    end
    on_intel do
      url "https://github.com/contember/tudy/releases/download/v#{version}/tudy-darwin-amd64.tar.gz"
      sha256 "956eb349d16dcff7eb3bd9977c52052980122685d20a0d1e7a144f5d6be3031d"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/contember/tudy/releases/download/v#{version}/tudy-linux-arm64.tar.gz"
      sha256 "80ab93ecfd41a804ed880bdb16f4d5947451dc88b4e5a0fd8e63e91b6c165843"
    end
    on_intel do
      url "https://github.com/contember/tudy/releases/download/v#{version}/tudy-linux-amd64.tar.gz"
      sha256 "83c68b0bc9006061c427e915714e5c6b6d72fa0fd548346b833f4c947cd56208"
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
