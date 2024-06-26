# score-eg-thumbnail-service

An example service that accepts reads a queue of image events and responds to them with generated thumbnails. This is used by <https://github.com/astromechza/score-eg-image-service>.

The message protocol is very simple:

## Input message

Exchange: "" (default exchange)

Routing Key: `thumbnail-generation` (OR the value of $AMQP_THUMBNAILING_ROUTING_KEY)

Message Body: the raw png, jpeg, or gif contents, the size can be up to the rabbitmq max message size (usually 128MB to 2GB)

Reply-To: the routing key to send the output message to

Message-Id: an optional message id to include in the logs -- use this to match responses to requests

## Output message

Exchange: "" (default exchange)

Routing Key: whatever the input message declared in Reply-To

Message Body: either the raw 200x200 jpeg bytes, or an errorCode

Correlation-Id: a copy of the message id that resulted in this response -- use this to match responses to requests

# Testing with Score

```
score-compose init
score-compose generate score.yaml --build main=. \
    --override-property 'resources.queue.metadata.annotations.compose\.score\.dev/publish-port="15672"' \
    --override-property 'resources.queue.metadata.annotations.compose\.score\.dev/publish-management-port="16672"'
docker compose up -d --build
score-compose resources get-outputs 'amqp.default#shared' | jq
```

Browse to the management interface at `http://localhost:16672/` login with the username and password and open the vhost with the unique ID.

Then send a new message on the default exchange to the `thumbnail-generation` routing key with the contents of `cat lenna.png | base64`.

Or run the automated test script:

```
AMQP_CONNECTION=$(score-compose resources get-outputs 'amqp.default#shared' --format 'amqp://{{.username}}:{{.password}}@localhost:15672/{{.vhost}}') \
    AMQP_THUMBNAILING_ROUTING_KEY=thumbnail-generation \
    go test ./
```

This will run the test and then write the output thumbnail to the local directory.

![library.jpg](samples/library.jpg)

![library_output.jpeg](samples/library_output.jpeg)
