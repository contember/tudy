class Tudy < Formula
  desc "AI-powered local development proxy"
  homepage "https://github.com/contember/tudy"
  version "0.7.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/contember/tudy/releases/download/v#{version}/tudy-darwin-arm64.tar.gz"
      sha256 "661633a1fdc772241fd5acf99191d66b9e163deae7f91c8b250a7b6a92ea73a9"
    end
    on_intel do
      url "https://github.com/contember/tudy/releases/download/v#{version}/tudy-darwin-amd64.tar.gz"
      sha256 "2ee99fef83c207fcc40c81c3707d7221f152c27ee61618f8f62b7d234549928f"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/contember/tudy/releases/download/v#{version}/tudy-linux-arm64.tar.gz"
      sha256 "88355c674445b3044b07b386c81bb98bad8a688dd7afcdc7bd24ab11b91be9d8"
    end
    on_intel do
      url "https://github.com/contember/tudy/releases/download/v#{version}/tudy-linux-amd64.tar.gz"
      sha256 "bd3b21f5d174ea605811239cc9448e582295051fca8ca6c824d49af4b5aac44d"
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
