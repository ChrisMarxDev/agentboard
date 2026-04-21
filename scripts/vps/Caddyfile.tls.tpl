{{DOMAIN}} {
	encode zstd gzip

	reverse_proxy agentboard:3000 {
		header_up X-Forwarded-Proto {scheme}
		header_up X-Forwarded-Host {host}
	}

	log {
		output stdout
		format console
	}
}
