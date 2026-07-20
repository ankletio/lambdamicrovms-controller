	clientToken, err := clientTokenFor(desired)
	if err != nil {
		return nil, ackerr.NewTerminalError(err)
	}
	input.ClientToken = aws.String(clientToken)
