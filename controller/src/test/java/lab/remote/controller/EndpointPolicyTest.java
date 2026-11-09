package lab.remote.controller;

public final class EndpointPolicyTest {
    public static void main(String[] args) {
        for (String value:new String[]{"localhost","127.0.0.1","10.0.0.1","192.168.1.20","172.16.0.1","phone.local"}) {
            if (!EndpointPolicy.isPrivateHost(value)) throw new AssertionError("Rejected private host: "+value);
        }
        for (String value:new String[]{"control.example.com","10.0.0.1.attacker.example","127.foo.0.0.1","010.0.0.1","192.168.1.999","172.32.0.1","10.0.0.1."}) {
            if (EndpointPolicy.isPrivateHost(value)) throw new AssertionError("Allowed public/ambiguous host: "+value);
        }
        System.out.println("Controller host policy passed: private addresses, public domains, suffix confusion and ambiguous numeric hosts.");
    }
}
