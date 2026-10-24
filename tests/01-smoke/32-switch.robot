*** Settings ***
Library             OperatingSystem
Library             Process
Resource            ../common.robot

Suite Setup         Run    ${CLAB_BIN} --runtime ${runtime} destroy -c -t ${CURDIR}/${lab-file}
Suite Teardown      Run    ${CLAB_BIN} --runtime ${runtime} destroy -c -t ${CURDIR}/${lab-file}


*** Variables ***
${lab-file}     32-switch.clab.yml
${lab-name}     switch
${runtime}      docker


*** Test Cases ***
Deploy ${lab-name} lab
    ${output} =    Process.Run Process
    ...    ${CLAB_BIN} --runtime ${runtime} deploy -t ${CURDIR}/${lab-file}
    ...    shell=True

    Log    stdout:${\n}${output.stdout}    console=${True}
    Log    stderr:${\n}${output.stderr}    console=${True}

    Should Be Equal As Integers    ${output.rc}    0

Ensure switch ports are attached to br0
    Ports Are Attached

Ensure IPv6 is disabled on switch ports
    ${rc}    ${output} =    Run And Return Rc And Output
    ...    sudo -E docker exec clab-${lab-name}-sw1 cat /proc/sys/net/ipv6/conf/eth1/disable_ipv6

    Should Be Equal As Integers    ${rc}    0
    Should Be Equal As Strings    ${output}    1

Ensure c1 can ping c2 over the switch
    Ping c2

Stop and start the switch
    ${output} =    Process.Run Process
    ...    ${CLAB_BIN} --runtime ${runtime} stop -t ${CURDIR}/${lab-file} --node sw1
    ...    shell=True
    Should Be Equal As Integers    ${output.rc}    0

    ${output} =    Process.Run Process
    ...    ${CLAB_BIN} --runtime ${runtime} start -t ${CURDIR}/${lab-file} --node sw1
    ...    shell=True
    Log    stderr:${\n}${output.stderr}    console=${True}
    Should Be Equal As Integers    ${output.rc}    0

Ensure switch forwards after the start
    Ports Are Attached
    Wait Until Keyword Succeeds    10x    1s    Ping c2


*** Keywords ***
Ports Are Attached
    FOR    ${port}    IN    eth1    eth2
        ${rc}    ${output} =    Run And Return Rc And Output
        ...    sudo -E docker exec clab-${lab-name}-sw1 ip link show dev ${port}
        Log    ${output}
        Should Be Equal As Integers    ${rc}    0
        Should Contain    ${output}    master br0
    END

Ping c2
    ${rc}    ${output} =    Run And Return Rc And Output
    ...    sudo -E docker exec clab-${lab-name}-c1 ping -c 1 -W 2 192.168.10.2
    Log    ${output}
    Should Be Equal As Integers    ${rc}    0
