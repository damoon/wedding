disable_snapshots()
analytics_settings(enable=False)
allow_k8s_contexts(os.getenv("TILT_ALLOW_CONTEXT"))

include('./service-dependencies/Tiltfile')
include('./tests/Tiltfile')

k8s_yaml('deployment/config.yaml')
k8s_yaml('deployment/kubernetes.yaml')

target='prod'
live_update=[]
if os.environ.get('PROD', '') ==  '':
  target='build-env'
  live_update=[
    sync('pkg',    '/app/pkg'),
    sync('cmd',    '/app/cmd'),
    sync('go.mod', '/app/go.mod'),
    sync('go.sum', '/app/go.sum'),
    run('go install -v ./cmd/wedding'),
  ]

docker_build(
  'davedamoon/wedding:latest',
  '.',
  dockerfile='deployment/Dockerfile',
  target=target,
  build_args={"SOURCE_BRANCH":"development", "SOURCE_COMMIT":"development"},
  only=[ 'go.mod'
       , 'go.sum'
       , 'pkg'
       , 'cmd'
       , 'deployment'
  ],
  ignore=[ '.git'
         , '*/*_test.go'
         , 'deployment/kubernetes.yaml'
  ],
  live_update=live_update,
)

k8s_resource(
  'wedding',
  port_forwards=['12375:2375'],
  resource_deps=['minio-buckets', 'registry', 'docker-hub-mirror'],
  labels=["application"],
)
