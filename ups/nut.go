package ups

import (
	"bufio"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// numericPattern is a compiled regexp for matching numeric values.
var numericPattern = regexp.MustCompile(`^-?[0-9.]+$`)

// Client contains information about the NUT server as well as the connection.
type NutClient struct {
	Version         string
	ProtocolVersion string
	Hostname        net.Addr
	conn            *net.TCPConn
}

// Connect accepts a hostname/IP string and an optional port, then creates a connection to NUT, returning a Client.
func NutConnect(hostname string, _port ...int) (NutClient, error) {
	port := 3493
	if len(_port) > 0 {
		port = _port[0]
	}
	tcpAddr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", hostname, port))
	if err != nil {
		return NutClient{}, err
	}
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		return NutClient{}, err
	}
	client := NutClient{
		Hostname: conn.RemoteAddr(),
		conn:     conn,
	}
	_, _ = client.GetVersion()
	_, _ = client.GetNetworkProtocolVersion()
	return client, nil
}

// Disconnect gracefully disconnects from NUT by sending the LOGOUT command.
func (c *NutClient) Disconnect() (bool, error) {
	logoutResp, err := c.SendCommand("LOGOUT")
	if err != nil {
		return false, err
	}
	if logoutResp[0] == "OK Goodbye" || logoutResp[0] == "Goodbye..." {
		return true, nil
	}
	return false, nil
}

// ReadResponse is a convenience function for reading newline delimited responses.
func (c *NutClient) ReadResponse(endLine string, multiLineResponse bool) (resp []string, err error) {
	_ = c.conn.SetReadDeadline(time.Now().Add(time.Second * 2))
	connbuff := bufio.NewReader(c.conn)
	response := []string{}

	for {
		line, err := connbuff.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("error reading response: %v", err)
		}
		if len(line) > 0 {
			cleanLine := strings.TrimSuffix(line, "\n")
			lines := strings.Split(cleanLine, "\n")
			response = append(response, lines...)
			if line == endLine || !multiLineResponse {
				break
			}
		}
	}

	return response, err
}

// SendCommand sends the string cmd to the device, and returns the response.
func (c *NutClient) SendCommand(cmd string) (resp []string, err error) {
	cmd = fmt.Sprintf("%v\n", cmd)
	endLine := fmt.Sprintf("END %s", cmd)
	if strings.HasPrefix(cmd, "USERNAME ") || strings.HasPrefix(cmd, "PASSWORD ") || strings.HasPrefix(cmd, "SET ") || strings.HasPrefix(cmd, "HELP ") || strings.HasPrefix(cmd, "VER ") || strings.HasPrefix(cmd, "NETVER ") {
		endLine = "OK\n"
	}
	_, err = fmt.Fprint(c.conn, cmd)
	if err != nil {
		return []string{}, err
	}

	resp, err = c.ReadResponse(endLine, strings.HasPrefix(cmd, "LIST "))
	if err != nil {
		return []string{}, err
	}

	if strings.HasPrefix(resp[0], "ERR ") {
		return []string{}, fmt.Errorf("NUT error: %s", strings.Split(resp[0], " ")[1])
	}

	return resp, nil
}

// Authenticate accepts a username and passwords and uses them to authenticate the existing NUT session.
func (c *NutClient) Authenticate(username, password string) (bool, error) {
	usernameResp, err := c.SendCommand(fmt.Sprintf("USERNAME %s", username))
	if err != nil {
		return false, err
	}
	passwordResp, err := c.SendCommand(fmt.Sprintf("PASSWORD %s", password))
	if err != nil {
		return false, err
	}
	if usernameResp[0] == "OK" && passwordResp[0] == "OK" {
		return true, nil
	}
	return false, nil
}

// GetUPSList returns a list of all UPSes provided by this NUT instance.
func (c *NutClient) GetUPSList() ([]NutUPS, error) {
	upsList := []NutUPS{}
	resp, err := c.SendCommand("LIST UPS")
	if err != nil {
		return upsList, err
	}
	for _, line := range resp {
		if strings.HasPrefix(line, "UPS ") {
			splitLine := strings.Split(strings.TrimPrefix(line, "UPS "), `"`)
			newUPS, err := NewNutUPS(strings.TrimSuffix(splitLine[0], " "), c)
			if err != nil {
				return upsList, err
			}
			upsList = append(upsList, newUPS)
		}
	}
	return upsList, err
}

// GetVersion returns the the version of the server currently in use.
func (c *NutClient) GetVersion() (string, error) {
	versionResponse, err := c.SendCommand("VER")
	if err != nil || len(versionResponse) < 1 {
		return "", err
	}
	c.Version = versionResponse[0]
	return versionResponse[0], err
}

// GetNetworkProtocolVersion returns the version of the network protocol currently in use.
func (c *NutClient) GetNetworkProtocolVersion() (string, error) {
	versionResponse, err := c.SendCommand("NETVER")
	if err != nil || len(versionResponse) < 1 {
		return "", err
	}
	c.ProtocolVersion = versionResponse[0]
	return versionResponse[0], err
}

// NutUPS contains information about a specific UPS provided by the NUT instance.
type NutUPS struct {
	Name      string
	Variables []NutVariable
	nutClient *NutClient
}

// NutVariable describes a single variable related to a UPS.
type NutVariable struct {
	Name  string
	Value interface{}
	Type  string
}

// NewNutUPS takes a UPS name and NUT client and returns an instantiated UPS struct.
func NewNutUPS(name string, client *NutClient) (NutUPS, error) {
	newUPS := NutUPS{
		Name:      name,
		nutClient: client,
	}
	_, err := newUPS.GetVariables()
	return newUPS, err
}

// GetVariables returns a slice of Variable structs for the UPS.
func (u *NutUPS) GetVariables() ([]NutVariable, error) {
	vars := []NutVariable{}
	resp, err := u.nutClient.SendCommand(fmt.Sprintf("LIST VAR %s", u.Name))
	if err != nil {
		return vars, err
	}
	offset := fmt.Sprintf("VAR %s ", u.Name)
	for _, line := range resp[1 : len(resp)-1] {
		newVar := NutVariable{}
		cleanedLine := strings.TrimPrefix(line, offset)
		splitLine := strings.Split(cleanedLine, `"`)
		splitLine[1] = strings.Trim(splitLine[1], " ")
		newVar.Name = strings.TrimSuffix(splitLine[0], " ")
		newVar.Value = splitLine[1]

		// Type conversion similar to original
		if numericPattern.MatchString(splitLine[1]) {
			if strings.Count(splitLine[1], ".") == 1 {
				converted, err := strconv.ParseFloat(splitLine[1], 64)
				if err == nil {
					newVar.Value = converted
					newVar.Type = "FLOAT_64"
				}
			} else {
				converted, err := strconv.ParseInt(splitLine[1], 10, 64)
				if err == nil {
					newVar.Value = converted
					newVar.Type = "INTEGER"
				}
			}
		}

		// Boolean check
		if splitLine[1] == "enabled" {
			newVar.Value = true
			newVar.Type = "BOOLEAN"
		}
		if splitLine[1] == "disabled" {
			newVar.Value = false
			newVar.Type = "BOOLEAN"
		}

		// Default to STRING
		if newVar.Type == "" {
			newVar.Type = "STRING"
		}

		vars = append(vars, newVar)
	}
	u.Variables = vars
	return vars, nil
}
