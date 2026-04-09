package no.uio.ifi.localega.doa.services;

import no.uio.ifi.localega.doa.exception.JsonSchemaValidationException;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.TestInstance;

import static org.junit.jupiter.api.Assertions.*;

@TestInstance(TestInstance.Lifecycle.PER_CLASS)
class JsonSchemaValidationServiceTest {

    // A syntactically valid JWT (header.payload.signature, all base64url characters)
    private static final String VALID_JWT = "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ0ZXN0In0.c2lnbmF0dXJl";

    // Meets publicKey minLength: 20
    private static final String VALID_PUBLIC_KEY = "AABBCCDDEEFFGGHHIIJJ";

    private JsonSchemaValidationService service;

    @BeforeAll
    void setUp() {
        service = new JsonSchemaValidationService();
    }

    @Test
    void validate_doesNotThrow_whenOnlyDatasetIdProvided() {
        String message = """
                {
                  "jwtToken": "%s",
                  "datasetId": "EGAD00010000919",
                  "publicKey": "%s"
                }
                """.formatted(VALID_JWT, VALID_PUBLIC_KEY);

        assertDoesNotThrow(() -> service.validate(message));
    }

    @Test
    void validate_doesNotThrow_whenOnlyFileIdProvided() {
        String message = """
                {
                  "jwtToken": "%s",
                  "fileId": "EGAF00000000014",
                  "publicKey": "%s"
                }
                """.formatted(VALID_JWT, VALID_PUBLIC_KEY);

        assertDoesNotThrow(() -> service.validate(message));
    }

    @Test
    void validate_doesNotThrow_whenBothDatasetIdAndFileIdProvided() {
        String message = """
                {
                  "jwtToken": "%s",
                  "datasetId": "EGAD00010000919",
                  "fileId": "EGAF00000000014",
                  "publicKey": "%s"
                }
                """.formatted(VALID_JWT, VALID_PUBLIC_KEY);

        assertDoesNotThrow(() -> service.validate(message));
    }

    @Test
    void validate_doesNotThrow_withOptionalCoordinates() {
        String message = """
                {
                  "jwtToken": "%s",
                  "datasetId": "EGAD00010000919",
                  "publicKey": "%s",
                  "startCoordinate": "100",
                  "endCoordinate": "200"
                }
                """.formatted(VALID_JWT, VALID_PUBLIC_KEY);

        assertDoesNotThrow(() -> service.validate(message));
    }

    @Test
    void validate_doesNotThrow_whenCoordinateIsZero() {
        String message = """
                {
                  "jwtToken": "%s",
                  "fileId": "EGAF00000000014",
                  "publicKey": "%s",
                  "startCoordinate": "0"
                }
                """.formatted(VALID_JWT, VALID_PUBLIC_KEY);

        assertDoesNotThrow(() -> service.validate(message));
    }

    @Test
    void validate_throws_whenJwtTokenIsMissing() {
        String message = """
                {
                  "datasetId": "EGAD00010000919",
                  "publicKey": "%s"
                }
                """.formatted(VALID_PUBLIC_KEY);

        assertThrows(JsonSchemaValidationException.class, () -> service.validate(message));
    }

    @Test
    void validate_throws_whenPublicKeyIsMissing() {
        String message = """
                {
                  "jwtToken": "%s",
                  "datasetId": "EGAD00010000919"
                }
                """.formatted(VALID_JWT);

        assertThrows(JsonSchemaValidationException.class, () -> service.validate(message));
    }

    @Test
    void validate_throws_whenNeitherDatasetIdNorFileIdIsPresent() {
        String message = """
                {
                  "jwtToken": "%s",
                  "publicKey": "%s"
                }
                """.formatted(VALID_JWT, VALID_PUBLIC_KEY);

        assertThrows(JsonSchemaValidationException.class, () -> service.validate(message));
    }

    @Test
    void validate_throws_whenJsonObjectIsEmpty() {
        assertThrows(JsonSchemaValidationException.class, () -> service.validate("{}"));
    }

    @Test
    void validate_throws_whenJwtTokenDoesNotMatchPattern() {
        String message = """
                {
                  "jwtToken": "not-a-valid-jwt",
                  "datasetId": "EGAD00010000919",
                  "publicKey": "%s"
                }
                """.formatted(VALID_PUBLIC_KEY);

        assertThrows(JsonSchemaValidationException.class, () -> service.validate(message));
    }

    @Test
    void validate_throws_whenDatasetIdIsEmpty() {
        String message = """
                {
                  "jwtToken": "%s",
                  "datasetId": "",
                  "publicKey": "%s"
                }
                """.formatted(VALID_JWT, VALID_PUBLIC_KEY);

        assertThrows(JsonSchemaValidationException.class, () -> service.validate(message));
    }

    @Test
    void validate_throws_whenFileIdIsEmpty() {
        String message = """
                {
                  "jwtToken": "%s",
                  "fileId": "",
                  "publicKey": "%s"
                }
                """.formatted(VALID_JWT, VALID_PUBLIC_KEY);

        assertThrows(JsonSchemaValidationException.class, () -> service.validate(message));
    }

    @Test
    void validate_throws_whenPublicKeyIsTooShort() {
        String message = """
                {
                  "jwtToken": "%s",
                  "datasetId": "EGAD00010000919",
                  "publicKey": "tooshort"
                }
                """.formatted(VALID_JWT);

        // "tooshort" is 8 chars, below minLength: 20
        assertThrows(JsonSchemaValidationException.class, () -> service.validate(message));
    }

    @Test
    void validate_throws_whenStartCoordinateIsNotNumeric() {
        String message = """
                {
                  "jwtToken": "%s",
                  "datasetId": "EGAD00010000919",
                  "publicKey": "%s",
                  "startCoordinate": "not-a-number"
                }
                """.formatted(VALID_JWT, VALID_PUBLIC_KEY);

        assertThrows(JsonSchemaValidationException.class, () -> service.validate(message));
    }

    @Test
    void validate_throws_whenEndCoordinateIsNegative() {
        String message = """
                {
                  "jwtToken": "%s",
                  "datasetId": "EGAD00010000919",
                  "publicKey": "%s",
                  "endCoordinate": "-100"
                }
                """.formatted(VALID_JWT, VALID_PUBLIC_KEY);

        // Negative numbers have a leading "-", which does not match pattern ^[0-9]+$
        assertThrows(JsonSchemaValidationException.class, () -> service.validate(message));
    }

    @Test
    void validate_throws_whenAdditionalPropertyIsPresent() {
        String message = """
                {
                  "jwtToken": "%s",
                  "datasetId": "EGAD00010000919",
                  "publicKey": "%s",
                  "unknownField": "value"
                }
                """.formatted(VALID_JWT, VALID_PUBLIC_KEY);

        assertThrows(JsonSchemaValidationException.class, () -> service.validate(message));
    }

    @Test
    void validate_exceptionMessage_includesValidationFailedPrefix() {
        String message = """
                {
                  "jwtToken": "%s",
                  "publicKey": "%s"
                }
                """.formatted(VALID_JWT, VALID_PUBLIC_KEY);

        JsonSchemaValidationException ex = assertThrows(JsonSchemaValidationException.class, () -> service.validate(message));
        assertTrue(ex.getMessage().startsWith("JSON Schema Validation Failed:"), "Expected message to start with 'JSON Schema Validation Failed:', got: " + ex.getMessage());
    }

    @Test
    void validate_exceptionMessage_includesAllViolations_whenMultipleConstraintsFail() {
        String message = "{}";

        JsonSchemaValidationException ex = assertThrows(JsonSchemaValidationException.class, () -> service.validate(message));
        // Multiple required fields are missing; the message should contain more than one error line
        long violationCount = ex.getMessage().lines().count();
        assertTrue(violationCount > 1, "Expected multiple violation lines, got: " + violationCount);
    }
}
