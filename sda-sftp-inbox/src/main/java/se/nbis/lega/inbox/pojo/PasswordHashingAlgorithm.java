package se.nbis.lega.inbox.pojo;

/**
 * Hashing algorithm for the password.
 */
public enum PasswordHashingAlgorithm {

    MD5("$1"),
    SHA256("$5"),
    SHA512("$6"),
    BLOWFISH(null);

    private final String magicString;

    PasswordHashingAlgorithm(String magicString) {
        this.magicString = magicString;
    }

    public String getMagicString() {
        return magicString;
    }

}
