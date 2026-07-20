# ACK service controller for Lambda MicroVMs

This repository contains source code for the AWS Controllers for Kubernetes
(ACK) service controller for Lambda MicroVMs.

Please [log issues][ack-issues] and feedback on the main AWS Controllers for
Kubernetes Github project.

[ack-issues]: https://github.com/aws/aws-controllers-k8s/issues

## Durable `RunMicrovm` idempotency

This fork keeps the upstream-compatible `Microvm` CRD and requires every
`Microvm` resource to carry the AWS idempotency token in metadata:

```yaml
metadata:
  annotations:
    courtx.ai/client-token: "<1-128 character stable token>"
```

The controller copies that value into `RunMicrovmInput.ClientToken` on every
create attempt. It also preserves the original Kubernetes spec after AWS
returns create-time defaults, so a retry after status loss sends the same token
and request parameters. Deployments should enforce the annotation's format and
immutability with admission policy.

When the controller uses IRSA/web-identity credentials, also set a stable
`AWS_ROLE_SESSION_NAME` on the controller Deployment. In live testing, two
identical requests in one controller process were idempotent, but a retry from
a replacement pod was rejected until both pods used the same assumed-role
session name. The AWS API reference does not currently document this scope.

## Contributing

We welcome community contributions and pull requests.

See our [contribution guide](/CONTRIBUTING.md) for more information on how to
report issues, set up a development environment, and submit code.

We adhere to the [Amazon Open Source Code of Conduct][coc].

You can also learn more about our [Governance](/GOVERNANCE.md) structure.

[coc]: https://aws.github.io/code-of-conduct

## License

This project is [licensed](/LICENSE) under the Apache-2.0 License.
