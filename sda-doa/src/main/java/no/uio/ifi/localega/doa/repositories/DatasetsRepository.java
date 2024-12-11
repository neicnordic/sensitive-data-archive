package no.uio.ifi.localega.doa.repositories;

import no.uio.ifi.localega.doa.model.Dataset;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

@Repository
public interface DatasetsRepository extends JpaRepository<Dataset, Integer> {
}
