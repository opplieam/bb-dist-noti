package tlsconfig

var (
	CAFile         = configFile("ca.pem")
	ServerCertFile = configFile("server.pem")
	ServerKeyFile  = configFile("server-key.pem")

	ClientCertFile = configFile("client.pem")
	ClientKeyFile  = configFile("client-key.pem")
)
